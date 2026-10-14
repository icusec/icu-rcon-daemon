package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	modeDaemon = "daemon"
	modeDirect = "direct"

	historyLimit = 200
	logLimit     = 800
)

// ServerEntry is one saved server. It is persisted verbatim to servers.json
// next to the executable so the next launch can quick-start.
type ServerEntry struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Mode       string    `json:"mode"` // modeDaemon | modeDirect
	DaemonHost string    `json:"daemonHost,omitempty"`
	DaemonPort int       `json:"daemonPort,omitempty"`
	AuthKey    string    `json:"authKey,omitempty"`
	RconHost   string    `json:"rconHost,omitempty"`
	RconPort   int       `json:"rconPort,omitempty"`
	RconPass   string    `json:"rconPass,omitempty"`
	Note       string    `json:"note,omitempty"`
	History    []string  `json:"history,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt,omitempty"`
}

// Address returns a human readable destination summary for lists and cards.
func (s *ServerEntry) Address() string {
	if s.Mode == modeDirect {
		return fmt.Sprintf("%s:%d", s.RconHost, s.RconPort)
	}
	return fmt.Sprintf("%s:%d → %s:%d", s.DaemonHost, s.DaemonPort, s.RconHost, s.RconPort)
}

type storeFile struct {
	Version      int            `json:"version"`
	LastSelected string         `json:"lastSelected,omitempty"`
	Theme        string         `json:"theme,omitempty"` // "" follow system, "dark", "light"
	Servers      []*ServerEntry `json:"servers"`
}

// themePref returns the saved theme preference: "", "dark" or "light".
func (s *store) themePref() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.Theme
}

// setThemePref stores the theme preference and persists the file.
func (s *store) setThemePref(v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.Theme = v
	return s.saveLocked()
}

// store keeps the server list in memory and mirrors every mutation to disk
// atomically (write temp file, then rename), so a crash never truncates the
// user's saved servers.
type store struct {
	mu   sync.Mutex
	path string
	data storeFile
}

func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func storePath() string { return filepath.Join(exeDir(), "servers.json") }

// tempStorePath is used by --selftest to avoid touching real user data.
func tempStorePath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("icu-rcon-selftest-%d.json", time.Now().UnixNano()))
}

// loadStore reads servers.json. A missing file yields an empty store and no
// error. A corrupted file is renamed aside (with a timestamp suffix) and the
// session continues with an empty list; the caller surfaces a warning.
func loadStore() (*store, string, error) { return loadStoreAt(storePath()) }

func loadStoreAt(path string) (*store, string, error) {
	s := &store{path: path, data: storeFile{Version: 1, Servers: []*ServerEntry{}}}
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, "", nil
		}
		return s, "", err
	}
	var f storeFile
	if err := json.Unmarshal(raw, &f); err != nil || f.Servers == nil {
		backup := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().Format("20060102-150405"))
		_ = os.Rename(s.path, backup)
		return s, backup, nil
	}
	if f.Version == 0 {
		f.Version = 1
	}
	s.data = f
	return s, "", nil
}

func (s *store) saveLocked() error {
	raw, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *store) list() []*ServerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*ServerEntry, len(s.data.Servers))
	copy(out, s.data.Servers)
	return out
}

func (s *store) get(id string) *ServerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.data.Servers {
		if e.ID == id {
			cp := *e
			return &cp
		}
	}
	return nil
}

func (s *store) lastSelected() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.LastSelected
}

func (s *store) setLastSelected(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data.LastSelected = id
	_ = s.saveLocked()
}

func (s *store) add(e *ServerEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.ID == "" {
		e.ID = newUUID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	s.data.Servers = append(s.data.Servers, e)
	s.data.LastSelected = e.ID
	return s.saveLocked()
}

func (s *store) update(id string, mut func(*ServerEntry)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.data.Servers {
		if e.ID == id {
			mut(e)
			return s.saveLocked()
		}
	}
	return fmt.Errorf("server %q not found", id)
}

func (s *store) delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data.Servers[:0]
	for _, e := range s.data.Servers {
		if e.ID != id {
			out = append(out, e)
		}
	}
	s.data.Servers = out
	if s.data.LastSelected == id {
		s.data.LastSelected = ""
	}
	return s.saveLocked()
}

// pushHistory records a command in the server's history (most recent first,
// duplicates collapsed to the front, bounded by historyLimit).
func (s *store) pushHistory(id, cmd string) error {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil
	}
	return s.update(id, func(e *ServerEntry) {
		out := make([]string, 0, historyLimit)
		out = append(out, cmd)
		for _, h := range e.History {
			if len(out) >= historyLimit {
				break
			}
			if h != cmd {
				out = append(out, h)
			}
		}
		e.History = out
		e.LastUsedAt = time.Now()
	})
}

// clearHistory wipes a server's command history.
func (s *store) clearHistory(id string) error {
	return s.update(id, func(e *ServerEntry) { e.History = nil })
}

// newUUID returns a random RFC 4122 version 4 UUID without external deps.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Practically unreachable; keep a deterministic fallback so the app
		// still functions if the system RNG ever fails.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (uint(i%8) * 8))
		}
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
