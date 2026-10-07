package statefile

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type memBackend struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memBackend) Read(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.data[key]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return d, nil
}

func (m *memBackend) Write(_ context.Context, key string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = append([]byte(nil), data...)
	return nil
}

func TestPlainFilesByDefault(t *testing.T) {
	Use(nil, "")
	path := filepath.Join(t.TempDir(), "a", "b.json")
	if _, err := ReadFile(path); !os.IsNotExist(err) {
		t.Fatalf("missing file: %v, want not-exist", err)
	}
	if err := WriteFile(path, []byte(`{"x":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ReadFile(path)
	if err != nil || string(got) != `{"x":1}` {
		t.Fatalf("read back %q, %v", got, err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}
}

func TestPathsUnderTheRootGoToTheBackend(t *testing.T) {
	root := t.TempDir()
	b := &memBackend{data: map[string][]byte{}}
	Use(b, root)
	t.Cleanup(func() { Use(nil, "") })

	in := filepath.Join(root, "operate", "queue.json")
	if _, err := ReadFile(in); !os.IsNotExist(err) || !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing state: %v, want not-exist for both os.IsNotExist and errors.Is", err)
	}
	if err := WriteFile(in, []byte("q"), 0o644); err != nil {
		t.Fatal(err)
	}
	if string(b.data["operate/queue.json"]) != "q" {
		t.Fatalf("backend got %v", b.data)
	}
	if _, err := os.Stat(in); !os.IsNotExist(err) {
		t.Fatal("a routed path must not also be written to disk")
	}

	out := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := WriteFile(out, []byte("f"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatal("a path outside the root stays a file")
	}
	if _, ok := b.data["../elsewhere.json"]; ok {
		t.Fatal("a path outside the root reached the backend")
	}
}
