package statefile

import (
	"context"
	"encoding/json"
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

func (m *memBackend) Update(_ context.Context, key string, mutate func(old []byte) ([]byte, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	next, err := mutate(append([]byte(nil), m.data[key]...))
	if err != nil {
		return err
	}
	m.data[key] = append([]byte(nil), next...)
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

func TestFileUpdateKeepsBothEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	bump := func(old []byte) ([]byte, error) {
		var rec struct {
			N int `json:"n"`
		}
		if len(old) > 0 {
			if err := json.Unmarshal(old, &rec); err != nil {
				return nil, err
			}
		}
		rec.N++
		return json.Marshal(rec)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Update(path, bump); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		N int `json:"n"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.N != 8 {
		t.Fatalf("n=%d, want 8 (each locked update applied)", rec.N)
	}
}

func TestUpdateDoesNotWriteWhenMutateFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Update(path, func([]byte) ([]byte, error) {
		return nil, errors.New("no")
	})
	if err == nil {
		t.Fatal("mutate error must surface")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "keep" {
		t.Fatalf("file is %q (%v), want the original bytes", got, err)
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
