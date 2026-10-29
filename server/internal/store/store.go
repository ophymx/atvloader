// Package store holds the APK catalog: metadata in a JSON index plus the
// APK files and icons on disk under a single data directory.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrProtected = errors.New("this app is ATV Loader itself and cannot be removed or hidden")
)

// Version is one uploaded APK for an app.
type Version struct {
	VersionCode int64     `json:"versionCode"`
	VersionName string    `json:"versionName"`
	File        string    `json:"file"` // relative to data dir
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256"`
	MinSDK      int32     `json:"minSdk"`
	TargetSDK   int32     `json:"targetSdk"`
	AddedAt     time.Time `json:"addedAt"`
	Changelog   string    `json:"changelog,omitempty"`
}

// App is one package with any number of versions, newest first.
type App struct {
	Package     string     `json:"package"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Category    string     `json:"category"`
	Featured    bool       `json:"featured"`
	Hidden      bool       `json:"hidden"`
	Protected   bool       `json:"protected"`      // the loader itself: cannot be hidden or removed
	Icon        string     `json:"icon,omitempty"` // relative to data dir
	AddedAt     time.Time  `json:"addedAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	Versions    []*Version `json:"versions"`
}

// Latest returns the highest versionCode, or nil when the app has no files.
func (a *App) Latest() *Version {
	if len(a.Versions) == 0 {
		return nil
	}
	return a.Versions[0]
}

func (a *App) clone() *App {
	c := *a
	c.Versions = make([]*Version, len(a.Versions))
	for i, v := range a.Versions {
		vc := *v
		c.Versions[i] = &vc
	}
	return &c
}

type index struct {
	Apps       map[string]*App    `json:"apps"`
	Categories []string           `json:"categories"`
	Devices    map[string]*Device `json:"devices"`
}

// Store is safe for concurrent use.
type Store struct {
	dir string
	mu  sync.RWMutex
	idx index

	pairOnce sync.Once
	pairing  *pairing
}

// Open loads or creates the catalog under dir.
func Open(dir string) (*Store, error) {
	for _, sub := range []string{"", "apks", "icons", "tmp"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return nil, err
		}
	}
	s := &Store{dir: dir, idx: index{Apps: map[string]*App{}}}
	b, err := os.ReadFile(s.indexPath())
	switch {
	case errors.Is(err, os.ErrNotExist):
		return s, s.save()
	case err != nil:
		return nil, err
	}
	if err := json.Unmarshal(b, &s.idx); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.indexPath(), err)
	}
	if s.idx.Apps == nil {
		s.idx.Apps = map[string]*App{}
	}
	if s.idx.Devices == nil {
		s.idx.Devices = map[string]*Device{}
	}
	for _, a := range s.idx.Apps {
		sortVersions(a)
	}
	return s, nil
}

func (s *Store) Dir() string           { return s.dir }
func (s *Store) indexPath() string     { return filepath.Join(s.dir, "store.json") }
func (s *Store) TempDir() string       { return filepath.Join(s.dir, "tmp") }
func (s *Store) Abs(rel string) string { return filepath.Join(s.dir, filepath.FromSlash(rel)) }

// save writes the index atomically. Caller holds the write lock.
func (s *Store) save() error {
	b, err := json.MarshalIndent(&s.idx, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.indexPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.indexPath())
}

func sortVersions(a *App) {
	sort.Slice(a.Versions, func(i, j int) bool { return a.Versions[i].VersionCode > a.Versions[j].VersionCode })
}

// List returns every app sorted by name.
func (s *Store) List() []*App {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*App, 0, len(s.idx.Apps))
	for _, a := range s.idx.Apps {
		out = append(out, a.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func (s *Store) Get(pkg string) (*App, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.idx.Apps[pkg]
	if !ok {
		return nil, ErrNotFound
	}
	return a.clone(), nil
}

// Categories returns the admin-defined list merged with any category in use.
func (s *Store) Categories() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.categoriesLocked()
}

func (s *Store) categoriesLocked() []string {
	seen := map[string]bool{}
	out := []string{}
	add := func(c string) {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			return
		}
		seen[c] = true
		out = append(out, c)
	}
	for _, c := range s.idx.Categories {
		add(c)
	}
	var extra []string
	for _, a := range s.idx.Apps {
		if c := strings.TrimSpace(a.Category); c != "" && !seen[c] {
			seen[c] = true
			extra = append(extra, c)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}

func (s *Store) SetCategories(cats []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Categories = s.idx.Categories[:0]
	for _, c := range cats {
		if c = strings.TrimSpace(c); c != "" {
			s.idx.Categories = append(s.idx.Categories, c)
		}
	}
	return s.save()
}

// AppPatch carries optional field updates; nil means leave unchanged.
type AppPatch struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Category    *string `json:"category"`
	Featured    *bool   `json:"featured"`
	Hidden      *bool   `json:"hidden"`
}

func (s *Store) UpdateApp(pkg string, p AppPatch) (*App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.idx.Apps[pkg]
	if !ok {
		return nil, ErrNotFound
	}
	if p.Name != nil && strings.TrimSpace(*p.Name) != "" {
		a.Name = strings.TrimSpace(*p.Name)
	}
	if p.Description != nil {
		a.Description = strings.TrimSpace(*p.Description)
	}
	if p.Category != nil {
		a.Category = strings.TrimSpace(*p.Category)
	}
	if p.Featured != nil {
		a.Featured = *p.Featured
	}
	if p.Hidden != nil {
		if *p.Hidden && a.Protected {
			return nil, ErrProtected
		}
		a.Hidden = *p.Hidden
	}
	a.UpdatedAt = time.Now()
	return a.clone(), s.save()
}

func (s *Store) UpdateVersion(pkg string, code int64, changelog string) (*App, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.idx.Apps[pkg]
	if !ok {
		return nil, ErrNotFound
	}
	for _, v := range a.Versions {
		if v.VersionCode == code {
			v.Changelog = strings.TrimSpace(changelog)
			a.UpdatedAt = time.Now()
			return a.clone(), s.save()
		}
	}
	return nil, ErrNotFound
}

func (s *Store) DeleteApp(pkg string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.idx.Apps[pkg]
	if !ok {
		return ErrNotFound
	}
	if a.Protected {
		return ErrProtected
	}
	for _, v := range a.Versions {
		os.Remove(s.Abs(v.File))
	}
	if a.Icon != "" {
		os.Remove(s.Abs(a.Icon))
	}
	delete(s.idx.Apps, pkg)
	return s.save()
}

// DeleteVersion removes one APK; deleting the last one removes the app.
func (s *Store) DeleteVersion(pkg string, code int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.idx.Apps[pkg]
	if !ok {
		return ErrNotFound
	}
	if a.Protected && len(a.Versions) == 1 {
		return ErrProtected
	}
	for i, v := range a.Versions {
		if v.VersionCode != code {
			continue
		}
		os.Remove(s.Abs(v.File))
		a.Versions = append(a.Versions[:i], a.Versions[i+1:]...)
		if len(a.Versions) == 0 {
			if a.Icon != "" {
				os.Remove(s.Abs(a.Icon))
			}
			delete(s.idx.Apps, pkg)
		} else {
			a.UpdatedAt = time.Now()
		}
		return s.save()
	}
	return ErrNotFound
}

// SetIcon replaces the app icon with a PNG read from r.
func (s *Store) SetIcon(pkg string, r io.Reader) (*App, error) {
	img, err := png.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("icon must be a PNG: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.idx.Apps[pkg]
	if !ok {
		return nil, ErrNotFound
	}
	rel := "icons/" + pkg + ".png"
	f, err := os.Create(s.Abs(rel))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return nil, err
	}
	a.Icon = rel
	a.UpdatedAt = time.Now()
	return a.clone(), s.save()
}

var safeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// ImportResult describes what an upload did.
type ImportResult struct {
	App       *App     `json:"app"`
	Version   *Version `json:"version"`
	Created   bool     `json:"created"`   // app did not exist before
	Replaced  bool     `json:"replaced"`  // same versionCode existed and was overwritten
	Unchanged bool     `json:"unchanged"` // identical file already present (SkipSameSHA)
}

// ImportOptions tweak how an upload is filed.
type ImportOptions struct {
	Protect     bool   // mark the app as the loader itself
	Name        string // name to use when the app is new (default: APK label)
	Description string // description to use when the app is new
	SkipSameSHA bool   // if an identical file is already stored, do nothing
}

// ImportAPK reads an APK from r, extracts its metadata, and files it.
func (s *Store) ImportAPK(r io.Reader, opts ...ImportOptions) (*ImportResult, error) {
	var o ImportOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	tmp, err := os.CreateTemp(s.TempDir(), "upload-*.apk")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	tmp.Close()
	if err != nil {
		return nil, err
	}
	meta, err := parseAPK(tmpName)
	if err != nil {
		return nil, fmt.Errorf("not a readable APK: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	res := &ImportResult{}
	sum := hex.EncodeToString(h.Sum(nil))
	a, ok := s.idx.Apps[meta.Package]
	if ok && o.SkipSameSHA {
		for _, v := range a.Versions {
			if v.SHA256 == sum {
				res.App, res.Version, res.Unchanged = a.clone(), v, true
				if o.Protect && !a.Protected {
					a.Protected = true
					return res, s.save()
				}
				return res, nil
			}
		}
	}
	if !ok {
		name := strings.TrimSpace(o.Name)
		if name == "" {
			name = strings.TrimSpace(meta.Label)
		}
		if name == "" {
			name = meta.Package
		}
		a = &App{Package: meta.Package, Name: name, Description: o.Description, AddedAt: now}
		s.idx.Apps[meta.Package] = a
		res.Created = true
	}
	if o.Protect {
		a.Protected = true
		a.Hidden = false
	}

	rel := fmt.Sprintf("apks/%s-%d.apk", safeName.ReplaceAllString(meta.Package, "_"), meta.VersionCode)
	if err := os.Rename(tmpName, s.Abs(rel)); err != nil {
		// cross-device fallback
		if err := copyFile(tmpName, s.Abs(rel)); err != nil {
			if res.Created {
				delete(s.idx.Apps, meta.Package)
			}
			return nil, err
		}
	}

	os.Chmod(s.Abs(rel), 0o644)

	v := &Version{
		VersionCode: meta.VersionCode,
		VersionName: meta.VersionName,
		File:        rel,
		Size:        size,
		SHA256:      sum,
		MinSDK:      meta.MinSDK,
		TargetSDK:   meta.TargetSDK,
		AddedAt:     now,
	}
	replaced := false
	for i, old := range a.Versions {
		if old.VersionCode == v.VersionCode {
			v.Changelog = old.Changelog
			a.Versions[i] = v
			replaced = true
			break
		}
	}
	if !replaced {
		a.Versions = append(a.Versions, v)
	}
	sortVersions(a)
	res.Replaced = replaced

	// Refresh the icon when this is the newest version (or we have none).
	if meta.Icon != nil && (a.Icon == "" || a.Versions[0] == v) {
		iconRel := "icons/" + safeName.ReplaceAllString(meta.Package, "_") + ".png"
		if f, err := os.Create(s.Abs(iconRel)); err == nil {
			if png.Encode(f, meta.Icon) == nil {
				a.Icon = iconRel
			}
			f.Close()
		}
	}
	a.UpdatedAt = now
	if err := s.save(); err != nil {
		return nil, err
	}
	res.App = a.clone()
	res.Version = v
	return res, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
