package server

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rgbyte/lattice/pkg/admin"
	"github.com/rgbyte/lattice/pkg/engine"
)

func TestServerConsoleAndBearerAuth(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "server_test.db")
	eng, err := engine.NewDefaultLocal(ctx, dbPath, 64)
	if err != nil {
		t.Fatalf("NewDefaultLocal: %v", err)
	}
	defer eng.Close()

	adm, err := admin.NewService(eng)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	if _, err := New(nil, adm, Config{}); err == nil {
		t.Fatalf("expected error when engine is nil")
	}

	srv, err := New(eng, adm, Config{
		AdminToken:   "secret-admin-token-123",
		MaxBodyBytes: 1024,
	})
	if err != nil {
		t.Fatalf("New server: %v", err)
	}

	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 1. GET / serves embedded HTML Console without requiring Bearer token
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}

	// 2. GET /api/v1/introspect/overview without token -> 401 Unauthorized
	unauthResp, err := http.Get(ts.URL + "/api/v1/introspect/overview")
	if err != nil {
		t.Fatalf("GET overview unauth: %v", err)
	}
	defer unauthResp.Body.Close()
	if unauthResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated API call status = %d, want 401", unauthResp.StatusCode)
	}

	// 3. GET /api/v1/introspect/overview with wrong token -> 401 Unauthorized
	reqWrong, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/introspect/overview", nil)
	reqWrong.Header.Set("Authorization", "Bearer wrong-token")
	wrongResp, err := http.DefaultClient.Do(reqWrong)
	if err != nil {
		t.Fatalf("GET overview wrong token: %v", err)
	}
	defer wrongResp.Body.Close()
	if wrongResp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong token API call status = %d, want 401", wrongResp.StatusCode)
	}

	// 4. GET /api/v1/introspect/overview with valid token -> 200 OK
	reqOK, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/introspect/overview", nil)
	reqOK.Header.Set("Authorization", "Bearer secret-admin-token-123")
	okResp, err := http.DefaultClient.Do(reqOK)
	if err != nil {
		t.Fatalf("GET overview valid token: %v", err)
	}
	defer okResp.Body.Close()
	if okResp.StatusCode != http.StatusOK {
		t.Errorf("valid token API call status = %d, want 200", okResp.StatusCode)
	}

	// 5. Oversized payload exceeding MaxBodyBytes (1024) -> 400 Bad Request
	largePayload := bytes.Repeat([]byte("A"), 2048)
	reqLarge, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/search", bytes.NewReader(largePayload))
	reqLarge.Header.Set("Authorization", "Bearer secret-admin-token-123")
	reqLarge.Header.Set("Content-Type", "application/json")
	largeResp, err := http.DefaultClient.Do(reqLarge)
	if err != nil {
		t.Fatalf("POST oversized payload: %v", err)
	}
	defer largeResp.Body.Close()
	if largeResp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized payload status = %d, want 400", largeResp.StatusCode)
	}
}
