// Package dynset ports modules/Global/dynamic_settings.py: runtime-changeable
// settings (the AI url and session id) that persist to dynamic_settings.json and
// fall back to the static config when unset.
package dynset

import (
	"encoding/json"
	"log/slog"
	"os"
	"sync"
)

// Settings manages the dynamic settings, mirroring the Python DynamicSettings
// singleton. It is safe for concurrent use (the admin command writes while the
// AI worker reads).
type Settings struct {
	path           string
	defaultURL     string
	defaultSession string

	mu       sync.RWMutex
	settings map[string]string
}

// New loads the settings file (if present) and records the config defaults.
func New(path, defaultURL, defaultSession string) *Settings {
	s := &Settings{
		path:           path,
		defaultURL:     defaultURL,
		defaultSession: defaultSession,
		settings:       map[string]string{},
	}
	s.load()
	return s
}

func (s *Settings) load() {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Warn("dynset: failed to load, using config defaults", "err", err)
		} else {
			slog.Info("dynset: no settings file found, using config defaults")
		}
		return
	}
	m := map[string]string{}
	if err := json.Unmarshal(b, &m); err != nil {
		slog.Warn("dynset: failed to parse, using config defaults", "err", err)
		return
	}
	s.settings = m
	slog.Info("dynset: loaded dynamic settings from file")
}

// save persists the current settings; the caller holds the write lock.
func (s *Settings) save() {
	b, err := json.MarshalIndent(s.settings, "", "  ")
	if err != nil {
		slog.Error("dynset: failed to marshal", "err", err)
		return
	}
	if err := os.WriteFile(s.path, b, 0o600); err != nil {
		slog.Error("dynset: failed to save", "err", err)
		return
	}
	slog.Info("dynset: saved dynamic settings to file")
}

func (s *Settings) get(key, def string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.settings[key]; ok {
		return v
	}
	return def
}

func (s *Settings) set(key, val string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings[key] = val
	s.save()
}

func (s *Settings) reset(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.settings[key]; ok {
		delete(s.settings, key)
		s.save()
	}
}

// AIURL returns the AI url, falling back to the config default.
func (s *Settings) AIURL() string { return s.get("ai_url", s.defaultURL) }

// SetAIURL sets and persists the AI url.
func (s *Settings) SetAIURL(url string) { s.set("ai_url", url) }

// ResetAIURL clears the AI url override.
func (s *Settings) ResetAIURL() { s.reset("ai_url") }

// AISessionID returns the AI session id, falling back to the config default.
func (s *Settings) AISessionID() string { return s.get("ai_session_id", s.defaultSession) }

// SetAISessionID sets and persists the AI session id.
func (s *Settings) SetAISessionID(id string) { s.set("ai_session_id", id) }

// ResetAISessionID clears the AI session id override.
func (s *Settings) ResetAISessionID() { s.reset("ai_session_id") }
