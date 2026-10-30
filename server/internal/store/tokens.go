package store

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// APIToken is a long-lived credential for automation (CI publishing an APK,
// a script approving TVs). Unlike the proxy-authenticated admin it carries
// scopes and an optional package allow-list.
type APIToken struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Prefix    string    `json:"prefix"` // start of the token, so a leaked one can be matched to its entry
	TokenHash string    `json:"tokenHash"`
	Scopes    []string  `json:"scopes"`
	Packages  []string  `json:"packages,omitempty"` // empty: any package
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	LastUsed  time.Time `json:"lastUsed,omitzero"`
}

// Scopes an API token can hold. Tokens can never manage tokens; that stays
// with a signed-in admin.
const (
	ScopeRead    = "read"    // list apps, download APKs and icons
	ScopePublish = "publish" // upload APKs, replace icons
	ScopeManage  = "manage"  // edit, hide, delete apps and versions; categories
	ScopeDevices = "devices" // approve, rename, unlink TVs
)

var AllScopes = []string{ScopeRead, ScopePublish, ScopeManage, ScopeDevices}

// TokenPrefix marks API tokens so the auth layer can tell them from device
// tokens without a lookup.
const TokenPrefix = "atv_"

var (
	ErrForbidden    = errors.New("forbidden")
	ErrInvalidScope = errors.New("unknown scope")
)

// Allows reports whether the token may perform scope on pkg. An empty pkg
// means the action is not about a specific package (listing, categories),
// which a package-restricted token may still do; handlers then filter.
func (t *APIToken) Allows(scope, pkg string) bool {
	if !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) {
		return false
	}
	ok := false
	for _, s := range t.Scopes {
		if s == scope {
			ok = true
			break
		}
	}
	if !ok {
		return false
	}
	if pkg == "" || len(t.Packages) == 0 {
		return true
	}
	for _, p := range t.Packages {
		if p == pkg {
			return true
		}
	}
	return false
}

func (t *APIToken) Expired() bool { return !t.ExpiresAt.IsZero() && time.Now().After(t.ExpiresAt) }

func normalizeScopes(scopes []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, s := range scopes {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "" || seen[s] {
			continue
		}
		valid := false
		for _, k := range AllScopes {
			if k == s {
				valid = true
			}
		}
		if !valid {
			return nil, fmt.Errorf("%w: %q", ErrInvalidScope, s)
		}
		seen[s] = true
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: a token needs at least one scope", ErrInvalidScope)
	}
	// Keep the canonical order for display.
	sort.Slice(out, func(i, j int) bool { return scopeRank(out[i]) < scopeRank(out[j]) })
	return out, nil
}

func scopeRank(s string) int {
	for i, k := range AllScopes {
		if k == s {
			return i
		}
	}
	return len(AllScopes)
}

func normalizePackages(pkgs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range pkgs {
		for _, q := range strings.FieldsFunc(p, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == ';' }) {
			q = strings.TrimSpace(q)
			if q == "" || seen[q] {
				continue
			}
			seen[q] = true
			out = append(out, q)
		}
	}
	sort.Strings(out)
	return out
}

// CreateToken mints a token. The plaintext is returned exactly once; only
// its hash is stored. ttl <= 0 means it never expires.
func (s *Store) CreateToken(name string, scopes, packages []string, ttl time.Duration, createdBy string) (*APIToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("a token needs a name")
	}
	sc, err := normalizeScopes(scopes)
	if err != nil {
		return nil, "", err
	}
	plain := TokenPrefix + randomToken(24)
	now := time.Now()
	t := &APIToken{
		ID: randomToken(8), Name: name, Prefix: plain[:len(TokenPrefix)+8], TokenHash: hashToken(plain),
		Scopes: sc, Packages: normalizePackages(packages), CreatedAt: now, CreatedBy: createdBy,
	}
	if ttl > 0 {
		t.ExpiresAt = now.Add(ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idx.Tokens == nil {
		s.idx.Tokens = map[string]*APIToken{}
	}
	s.idx.Tokens[t.ID] = t
	if err := s.save(); err != nil {
		delete(s.idx.Tokens, t.ID)
		return nil, "", err
	}
	tt := *t
	return &tt, plain, nil
}

// Tokens lists tokens, newest first. Hashes are included; callers strip them.
func (s *Store) Tokens() []*APIToken {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*APIToken, 0, len(s.idx.Tokens))
	for _, t := range s.idx.Tokens {
		tt := *t
		out = append(out, &tt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) RevokeToken(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.idx.Tokens[id]; !ok {
		return ErrNotFound
	}
	delete(s.idx.Tokens, id)
	return s.save()
}

// AuthenticateAPIToken resolves a plaintext token. Expired tokens do not
// authenticate. LastUsed is persisted at most once a minute.
func (s *Store) AuthenticateAPIToken(plain string) (*APIToken, bool) {
	if !strings.HasPrefix(plain, TokenPrefix) {
		return nil, false
	}
	h := hashToken(plain)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.idx.Tokens {
		if t.TokenHash != h {
			continue
		}
		if t.Expired() {
			return nil, false
		}
		now := time.Now()
		if now.Sub(t.LastUsed) > time.Minute {
			t.LastUsed = now
			_ = s.save()
		}
		tt := *t
		return &tt, true
	}
	return nil, false
}
