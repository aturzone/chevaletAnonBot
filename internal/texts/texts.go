// Package texts ports modules/Global/fetch_texts.py: it serves the static
// message templates from the Texts/ directory.
//
// The Python fetch_text re-read the file on every call; since these templates
// are static (mounted read-only in production) this loader caches them on first
// read instead.
package texts

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Loader serves trimmed text templates from a root directory, caching each one.
type Loader struct {
	root  string
	mu    sync.RWMutex
	cache map[string]string
}

// New returns a Loader rooted at dir (e.g. "Texts").
func New(dir string) *Loader {
	return &Loader{root: dir, cache: make(map[string]string)}
}

// Get returns the trimmed contents of <root>/<name>.txt. name may include a
// subdirectory, e.g. "settings/main". Mirrors fetch_text("settings/main").
func (l *Loader) Get(name string) (string, error) {
	l.mu.RLock()
	v, ok := l.cache[name]
	l.mu.RUnlock()
	if ok {
		return v, nil
	}

	b, err := os.ReadFile(filepath.Join(l.root, filepath.FromSlash(name)+".txt"))
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(b))

	l.mu.Lock()
	l.cache[name] = s
	l.mu.Unlock()
	return s, nil
}
