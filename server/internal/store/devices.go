package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// Device is a TV that completed the activation-code flow and holds a bearer token.
type Device struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	TokenHash  string    `json:"tokenHash"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeen   time.Time `json:"lastSeen"`
	ApprovedBy string    `json:"approvedBy"`
}

// PendingCode is an activation request waiting for an admin to approve it.
type PendingCode struct {
	DeviceCode string    `json:"-"`
	UserCode   string    `json:"userCode"`
	Model      string    `json:"model"`
	Product    string    `json:"product"`
	SDK        int       `json:"sdk"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	Status     string    `json:"status"` // pending | approved | denied
	token      string    // raw token, handed to the device exactly once
	device     *Device
}

const (
	codeTTL      = 15 * time.Minute
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
	PollInterval = 3
)

var (
	ErrCodeExpired = errors.New("code expired")
	ErrCodeUnknown = errors.New("unknown code")
)

// pairing lives in memory; codes are short-lived so nothing is lost on restart.
type pairing struct {
	mu    sync.Mutex
	codes map[string]*PendingCode // keyed by userCode
}

func (s *Store) pair() *pairing {
	s.pairOnce.Do(func() { s.pairing = &pairing{codes: map[string]*PendingCode{}} })
	return s.pairing
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func (p *pairing) pruneLocked() {
	now := time.Now()
	for k, c := range p.codes {
		// Approved codes linger a little so a slow poller still gets its token.
		if now.After(c.ExpiresAt) || (c.Status != "pending" && now.After(c.CreatedAt.Add(codeTTL+5*time.Minute))) {
			delete(p.codes, k)
		}
	}
}

// NewDeviceCode starts an activation: the TV shows UserCode, polls with DeviceCode.
func (s *Store) NewDeviceCode(model, product string, sdk int) *PendingCode {
	p := s.pair()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	var user string
	for {
		b := make([]byte, 6)
		rand.Read(b)
		var sb strings.Builder
		for i, x := range b {
			if i == 3 {
				sb.WriteByte('-')
			}
			sb.WriteByte(codeAlphabet[int(x)%len(codeAlphabet)])
		}
		user = sb.String()
		if _, taken := p.codes[user]; !taken {
			break
		}
	}
	now := time.Now()
	c := &PendingCode{
		DeviceCode: randomToken(32), UserCode: user, Model: strings.TrimSpace(model), Product: strings.TrimSpace(product), SDK: sdk,
		CreatedAt: now, ExpiresAt: now.Add(codeTTL), Status: "pending",
	}
	p.codes[user] = c
	return c
}

// PollDeviceCode is called by the TV. It returns the status and, once approved,
// the raw token exactly once.
func (s *Store) PollDeviceCode(deviceCode string) (status string, token string, dev *Device, err error) {
	p := s.pair()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	for k, c := range p.codes {
		if c.DeviceCode != deviceCode {
			continue
		}
		switch c.Status {
		case "approved":
			delete(p.codes, k)
			return "approved", c.token, c.device, nil
		case "denied":
			delete(p.codes, k)
			return "denied", "", nil, nil
		default:
			return "pending", "", nil, nil
		}
	}
	return "", "", nil, ErrCodeUnknown
}

// PendingCodes lists codes the admin UI can approve.
func (s *Store) PendingCodes() []*PendingCode {
	p := s.pair()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	out := make([]*PendingCode, 0, len(p.codes))
	for _, c := range p.codes {
		if c.Status == "pending" {
			cc := *c
			out = append(out, &cc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	code = strings.NewReplacer("-", "", " ", "", "O", "0", "I", "1", "L", "1").Replace(code)
	// The alphabet has no 0/1 either, so map those back to letters that are in it.
	code = strings.NewReplacer("0", "O", "1", "I").Replace(code)
	// After normalization the canonical form is XXX-XXX. Letters O/I never
	// appear in generated codes, so a typo of O for 0 cannot collide.
	if len(code) == 6 {
		return code[:3] + "-" + code[3:]
	}
	return code
}

// ApproveCode registers a device for the given user code.
func (s *Store) ApproveCode(userCode, name, approvedBy string) (*Device, error) {
	p := s.pair()
	p.mu.Lock()
	defer p.mu.Unlock()
	p.pruneLocked()
	c, ok := p.codes[lookupCode(p, userCode)]
	if !ok {
		return nil, ErrCodeUnknown
	}
	if c.Status != "pending" {
		return nil, fmt.Errorf("code already %s", c.Status)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = c.Model
	}
	if name == "" {
		name = "TV " + c.UserCode
	}
	token := randomToken(32)
	now := time.Now()
	dev := &Device{
		ID: randomToken(8), Name: name, Model: c.Model, TokenHash: hashToken(token),
		CreatedAt: now, LastSeen: now, ApprovedBy: approvedBy,
	}
	s.mu.Lock()
	if s.idx.Devices == nil {
		s.idx.Devices = map[string]*Device{}
	}
	s.idx.Devices[dev.ID] = dev
	err := s.save()
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	c.Status = "approved"
	c.token = token
	c.device = dev
	d := *dev
	return &d, nil
}

func lookupCode(p *pairing, userCode string) string {
	want := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(userCode), "-", ""), " ", ""))
	for k := range p.codes {
		if strings.ReplaceAll(k, "-", "") == want {
			return k
		}
	}
	return normalizeUserCode(userCode)
}

func (s *Store) DenyCode(userCode string) error {
	p := s.pair()
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.codes[lookupCode(p, userCode)]
	if !ok {
		return ErrCodeUnknown
	}
	c.Status = "denied"
	return nil
}

func (s *Store) Devices() []*Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Device, 0, len(s.idx.Devices))
	for _, d := range s.idx.Devices {
		dd := *d
		out = append(out, &dd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (s *Store) RenameDevice(id, name string) (*Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.idx.Devices[id]
	if !ok {
		return nil, ErrNotFound
	}
	if name = strings.TrimSpace(name); name != "" {
		d.Name = name
	}
	dd := *d
	return &dd, s.save()
}

func (s *Store) RevokeDevice(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.idx.Devices[id]; !ok {
		return ErrNotFound
	}
	delete(s.idx.Devices, id)
	return s.save()
}

// AuthenticateToken resolves a bearer token to its device and records activity.
func (s *Store) AuthenticateToken(token string) (*Device, bool) {
	if token == "" {
		return nil, false
	}
	h := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.idx.Devices {
		if d.TokenHash == h {
			now := time.Now()
			if now.Sub(d.LastSeen) > time.Minute {
				d.LastSeen = now
				_ = s.save()
			}
			dd := *d
			return &dd, true
		}
	}
	return nil, false
}
