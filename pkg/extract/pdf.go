package extract

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/hex"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/rgbyte/lattice/pkg/types"
)

// PDFMetadata holds extracted document-level metadata and structured Markdown-normalized text from a PDF.
type PDFMetadata struct {
	Title      string
	Author     string
	PageCount  int
	Content    string
}

var (
	rePDFPage       = regexp.MustCompile(`/Type\s*/Page\b`)
	rePDFTitle      = regexp.MustCompile(`/Title\s*\(([^)\\]*(?:\\.[^)\\]*)*)\)`)
	rePDFAuthor     = regexp.MustCompile(`/Author\s*\(([^)\\]*(?:\\.[^)\\]*)*)\)`)
	rePDFStream     = regexp.MustCompile(`(?s)<<(.*?)>>\s*stream\r?\n(.*?)\r?\nendstream`)
	rePDFTextBlock  = regexp.MustCompile(`(?s)\bBT\b(.*?)\bET\b`)
	rePDFTjArray    = regexp.MustCompile(`(?s)\[(.*?)\]\s*TJ`)
	rePDFTjLiteral  = regexp.MustCompile(`\(([^)\\]*(?:\\.[^)\\]*)*)\)\s*(?:Tj|'|")`)
	rePDFHexLiteral = regexp.MustCompile(`<([0-9A-Fa-f\s]+)>\s*Tj`)
	rePDFNumberedH  = regexp.MustCompile(`^(\d+(?:\.\d+){0,2}\.?)\s+([A-Z][A-Za-z0-9\s,&:/_-]{3,70})$`)
)

// ExtractPDFBytes extracts text, inferred section headings, title, author, and page count from raw PDF bytes.
// It uses a pure-Go PDF object/stream parser (supporting raw and FlateDecode/zlib compressed streams,
// literal/array/hex text operators, and PDF escape sequences) so it operates with zero external C dependencies.
func ExtractPDFBytes(raw []byte) (PDFMetadata, error) {
	if len(raw) < 8 || !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("%PDF")) {
		return PDFMetadata{}, fmt.Errorf("%w: invalid PDF header", types.ErrInvalidInput)
	}

	var meta PDFMetadata
	meta.PageCount = len(rePDFPage.FindAllIndex(raw, -1))
	if meta.PageCount == 0 {
		meta.PageCount = 1
	}

	if m := rePDFTitle.FindSubmatch(raw); len(m) > 1 {
		meta.Title = strings.TrimSpace(unescapePDFString(string(m[1])))
	}
	if m := rePDFAuthor.FindSubmatch(raw); len(m) > 1 {
		meta.Author = strings.TrimSpace(unescapePDFString(string(m[1])))
	}

	var extractedPages []string

	// Parse all PDF stream objects (both uncompressed and /FlateDecode compressed)
	streamMatches := rePDFStream.FindAllSubmatch(raw, -1)
	for _, sm := range streamMatches {
		dict := string(sm[1])
		streamBytes := sm[2]

		decoded := streamBytes
		if strings.Contains(dict, "FlateDecode") {
			if d, err := inflatePDFStream(streamBytes); err == nil && len(d) > 0 {
				decoded = d
			} else {
				continue
			}
		}

		text := extractTextFromContentStream(decoded)
		if strings.TrimSpace(text) != "" {
			extractedPages = append(extractedPages, text)
		}
	}

	// Fallback: if a simple PDF places BT...ET outside matched stream regex, scan raw bytes directly
	if len(extractedPages) == 0 {
		if fallback := extractTextFromContentStream(raw); strings.TrimSpace(fallback) != "" {
			extractedPages = append(extractedPages, fallback)
		}
	}

	combined := strings.Join(extractedPages, "\n\n")
	meta.Content = normalizePDFHeadings(combined)
	if strings.TrimSpace(meta.Content) == "" {
		return meta, fmt.Errorf("%w: no extractable text found in PDF", types.ErrInvalidInput)
	}
	if meta.Title == "" {
		// Infer title from first non-empty line or heading
		for _, line := range strings.Split(meta.Content, "\n") {
			clean := strings.TrimSpace(strings.TrimLeft(line, "# "))
			if len(clean) >= 4 && len(clean) <= 120 {
				meta.Title = clean
				break
			}
		}
	}
	return meta, nil
}

// ExtractPDFFile extracts text and metadata from a PDF file on disk, using pdftotext if installed
// for complex font encodings and falling back to the pure-Go ExtractPDFBytes parser.
func ExtractPDFFile(path string, raw []byte) (PDFMetadata, error) {
	meta, err := ExtractPDFBytes(raw)
	if pdftotextBin, lookErr := exec.LookPath("pdftotext"); lookErr == nil && path != "" {
		cmd := exec.Command(pdftotextBin, "-layout", "-enc", "UTF-8", path, "-")
		if out, cmdErr := cmd.Output(); cmdErr == nil {
			txt := normalizePDFHeadings(string(out))
			if len(strings.TrimSpace(txt)) > len(strings.TrimSpace(meta.Content)) {
				meta.Content = txt
				err = nil
			}
		}
	}
	return meta, err
}

func inflatePDFStream(data []byte) ([]byte, error) {
	// Try standard zlib header first (RFC 1950)
	if zr, err := zlib.NewReader(bytes.NewReader(data)); err == nil {
		defer zr.Close()
		if out, err := io.ReadAll(io.LimitReader(zr, 8<<20)); err == nil && len(out) > 0 {
			return out, nil
		}
	}
	// Fallback to raw deflate (RFC 1951)
	fr := flate.NewReader(bytes.NewReader(data))
	defer fr.Close()
	return io.ReadAll(io.LimitReader(fr, 8<<20))
}

func extractTextFromContentStream(stream []byte) string {
	s := string(stream)
	blocks := rePDFTextBlock.FindAllStringSubmatch(s, -1)
	if len(blocks) == 0 {
		return ""
	}

	var out strings.Builder
	for _, b := range blocks {
		blockBody := b[1]
		lines := strings.Split(strings.ReplaceAll(blockBody, "\r\n", "\n"), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Handle TJ arrays: [(Hello ) -20 (World)] TJ
			for _, tjArr := range rePDFTjArray.FindAllStringSubmatch(line, -1) {
				arrContent := tjArr[1]
				out.WriteString(extractStringsFromTJArray(arrContent))
			}

			// Handle literal (text) Tj
			for _, tjLit := range rePDFTjLiteral.FindAllStringSubmatch(line, -1) {
				out.WriteString(unescapePDFString(tjLit[1]))
			}

			// Handle <hex> Tj
			for _, hexLit := range rePDFHexLiteral.FindAllStringSubmatch(line, -1) {
				cleanHex := strings.Map(func(r rune) rune {
					if unicode.IsSpace(r) {
						return -1
					}
					return r
				}, hexLit[1])
				if decodedHex, err := hex.DecodeString(cleanHex); err == nil {
					out.WriteString(cleanPrintable(string(decodedHex)))
				}
			}

			// Line break operators (T*, Td, TD, ')
			if strings.HasSuffix(line, "T*") || strings.Contains(line, " Td") || strings.Contains(line, " TD") {
				out.WriteByte('\n')
			} else {
				out.WriteByte(' ')
			}
		}
		out.WriteByte('\n')
	}
	return strings.TrimSpace(out.String())
}

func extractStringsFromTJArray(arr string) string {
	var b strings.Builder
	inParen := false
	escaped := false
	var cur strings.Builder

	for i := 0; i < len(arr); i++ {
		ch := arr[i]
		if escaped {
			cur.WriteByte('\\')
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' && inParen {
			escaped = true
			continue
		}
		if ch == '(' && !inParen {
			inParen = true
			cur.Reset()
			continue
		}
		if ch == ')' && inParen {
			inParen = false
			b.WriteString(unescapePDFString(cur.String()))
			continue
		}
		if inParen {
			cur.WriteByte(ch)
		} else if ch == '-' {
			// Large negative kerning offset in TJ array represents a word space (e.g. -150)
			j := i + 1
			for j < len(arr) && (arr[j] >= '0' && arr[j] <= '9' || arr[j] == '.') {
				j++
			}
			if val, err := strconv.ParseFloat(arr[i+1:j], 64); err == nil && val >= 100 {
				b.WriteByte(' ')
			}
			i = j - 1
		}
	}
	return b.String()
}

func unescapePDFString(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 >= len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '(', ')', '\\':
			b.WriteByte(s[i])
		default:
			// Octal escape \ddd
			if s[i] >= '0' && s[i] <= '7' {
				oct := string(s[i])
				for k := 0; k < 2 && i+1 < len(s) && s[i+1] >= '0' && s[i+1] <= '7'; k++ {
					i++
					oct += string(s[i])
				}
				if val, err := strconv.ParseUint(oct, 8, 8); err == nil {
					b.WriteByte(byte(val))
				}
			} else {
				b.WriteByte(s[i])
			}
		}
	}
	return cleanPrintable(b.String())
}

func cleanPrintable(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= 32 && r != 127) {
			return r
		}
		return -1
	}, s)
}

// normalizePDFHeadings converts numbered PDF section headers (e.g. "1. Zero-Trust Architecture"
// or "2.1 Token Verification") into Markdown headings so ChunkAndExtractStructural builds section hierarchies and Topic nodes.
func normalizePDFHeadings(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			out = append(out, "")
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			out = append(out, trimmed)
			continue
		}
		if m := rePDFNumberedH.FindStringSubmatch(trimmed); m != nil {
			numPart := strings.TrimSuffix(m[1], ".")
			dots := strings.Count(numPart, ".")
			hashes := "##"
			if dots == 0 {
				hashes = "#"
			} else if dots >= 2 {
				hashes = "###"
			}
			out = append(out, fmt.Sprintf("%s %s", hashes, strings.TrimSpace(m[2])))
			continue
		}
		out = append(out, trimmed)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
