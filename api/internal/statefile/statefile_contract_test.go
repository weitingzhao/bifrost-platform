package statefile_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/statefile"
)

// Origin: 2026-11-16, the review of the foundation code found the base's
// core packages tested from the inside only. These are statefile's promises
// to the thirteen packages that keep their state through it, from outside
// the package. They do not run in parallel: the backend is the process's.

// memory is a Backend that keeps its keys in a map and records them.
type memory struct {
	mu   sync.Mutex
	data map[string][]byte
	keys []string
}

func (m *memory) Read(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = append(m.keys, "read "+key)
	v, ok := m.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return v, nil
}

func (m *memory) Write(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = append(m.keys, "write "+key)
	m.data[key] = data
	return nil
}

func (m *memory) Update(_ context.Context, key string, mutate func([]byte) ([]byte, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys = append(m.keys, "update "+key)
	next, err := mutate(m.data[key])
	if err != nil {
		return err
	}
	m.data[key] = next
	return nil
}

// Without a backend a path is a plain file: missing state reads as
// not-exist in both of the ways callers ask, a write makes its directory,
// and an update of a missing file starts from nil and writes nothing when
// the caller's function fails.
func TestPlainFileContract(t *testing.T) {
	statefile.Use(nil, "")
	path := filepath.Join(t.TempDir(), "deep", "state.json")
	if _, err := statefile.ReadFile(path); !errors.Is(err, fs.ErrNotExist) || !os.IsNotExist(err) {
		t.Fatalf("a missing file: %v", err)
	}
	failed := errors.New("no")
	if err := statefile.Update(path, func(old []byte) ([]byte, error) {
		if old != nil {
			t.Errorf("a missing file was handed to mutate as %q, not nil", old)
		}
		return []byte("x"), failed
	}); !errors.Is(err, failed) {
		t.Fatalf("mutate's error was not returned: %v", err)
	}
	if _, err := statefile.ReadFile(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a failed update wrote: %v", err)
	}
	if err := statefile.WriteFile(path, []byte("one"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := statefile.Update(path, func(old []byte) ([]byte, error) { return append(old, []byte("+two")...), nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := statefile.ReadFile(path); err != nil || string(got) != "one+two" {
		t.Fatalf("after a write and an update: %q, %v", got, err)
	}
}

// With a backend, a path under its root goes to the backend under the key
// relative to the root with forward slashes; a path outside the root stays a
// file; and the backend's not-exist is the caller's not-exist.
func TestBackendContract(t *testing.T) {
	root := t.TempDir()
	m := &memory{data: map[string][]byte{}}
	statefile.Use(m, root)
	t.Cleanup(func() { statefile.Use(nil, "") })
	if active, at := statefile.Active(); !active || at != filepath.Clean(root) {
		t.Fatalf("Active = %v, %q", active, at)
	}
	inside := filepath.Join(root, "operate", "queue.json")
	if _, err := statefile.ReadFile(inside); !errors.Is(err, fs.ErrNotExist) || !os.IsNotExist(err) {
		t.Fatalf("a key the backend does not have: %v", err)
	}
	if err := statefile.WriteFile(inside, []byte("a"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := statefile.Update(inside, func(old []byte) ([]byte, error) { return append(old, 'b'), nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := statefile.ReadFile(inside); err != nil || string(got) != "ab" {
		t.Fatalf("through the backend: %q, %v", got, err)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatalf("a path under the root was also written as a file: %v", err)
	}
	for _, k := range m.keys {
		if k[len(k)-len("operate/queue.json"):] != "operate/queue.json" {
			t.Fatalf("the backend was asked for %q, not the key relative to the root", k)
		}
	}
	// The positive control: outside the root the backend is not asked.
	asked := len(m.keys)
	outside := filepath.Join(t.TempDir(), "other.json")
	if err := statefile.WriteFile(outside, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(outside); err != nil || string(got) != "file" || len(m.keys) != asked {
		t.Fatalf("a path outside the root: %q, %v, backend asked %d more times", got, err, len(m.keys)-asked)
	}
}
