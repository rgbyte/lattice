package types

import "strings"

// NodeFilterFunc evaluates whether a given node or chunk ID is accessible to the caller.
type NodeFilterFunc func(id string) bool

// PrincipalSet represents the authenticated security identity executing a query.
type PrincipalSet struct {
	UserID      string              `json:"user_id"`
	UserEmail   string              `json:"user_email"`
	Domain      string              `json:"domain"`
	Groups      map[string]struct{} `json:"-"`
	AllowPublic bool                `json:"allow_public"`
}

// NewPrincipalSet constructs a normalized PrincipalSet.
func NewPrincipalSet(userID, userEmail string, groups []string) PrincipalSet {
	email := strings.ToLower(strings.TrimSpace(userEmail))
	domain := ""
	if idx := strings.IndexByte(email, '@'); idx != -1 && idx+1 < len(email) {
		domain = email[idx+1:]
	}
	gmap := make(map[string]struct{}, len(groups))
	for _, g := range groups {
		norm := strings.ToLower(strings.TrimSpace(g))
		norm = strings.TrimPrefix(norm, "group:")
		if norm != "" {
			gmap[norm] = struct{}{}
		}
	}
	return PrincipalSet{
		UserID:      strings.ToLower(strings.TrimSpace(userID)),
		UserEmail:   email,
		Domain:      domain,
		Groups:      gmap,
		AllowPublic: true,
	}
}

// CanAccess evaluates a resource's ACL permission list using a strict FAIL-CLOSED policy:
// if permissions is empty or contains no matching rule, access is denied.
func (p PrincipalSet) CanAccess(permissions []string) bool {
	if len(permissions) == 0 {
		return false
	}
	for _, raw := range permissions {
		rule := strings.ToLower(strings.TrimSpace(raw))
		if rule == "" {
			continue
		}
		if p.AllowPublic && (rule == "*" || rule == "public") {
			return true
		}
		if p.UserEmail != "" && (rule == p.UserEmail || rule == "user:"+p.UserEmail) {
			return true
		}
		if p.UserID != "" && (rule == p.UserID || rule == "person:"+p.UserID || rule == "user:"+p.UserID) {
			return true
		}
		if p.Domain != "" && rule == "*@"+p.Domain {
			return true
		}
		if strings.HasPrefix(rule, "group:") {
			grp := strings.TrimPrefix(rule, "group:")
			if _, ok := p.Groups[grp]; ok {
				return true
			}
		} else if _, ok := p.Groups[rule]; ok {
			return true
		}
	}
	return false
}
