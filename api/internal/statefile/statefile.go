// Package statefile is where platform-api keeps its small JSON state files
// (operate queue, checklist signals, release gate and cycles, patrol state,
// audit log, ...).
//
// Stores call ReadFile / WriteFile with the same paths they always used. By
// default those are plain files. In the cluster a Backend is installed for the
// data directory, and every path under it is stored there instead: until
// 2026-10-07 the cluster pods kept these files on an emptyDir, so each rollout
// erased the release history, the queue, the checklist and the audit trail,
// and the api and workers pods could not see each other's state (TD-196).
//
// This package imports no Kubernetes code: the operator plane and the local
// dev server use it as a plain file layer (the cluster backend lives in
// statefile/k8sstate and is wired by the server only).
package statefile

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Backend stores state by key (the path relative to the data directory, with
// forward slashes, e.g. "operate/queue.json").
type Backend interface {
	// Read returns fs.ErrNotExist (wrapped or bare) when the key has no state.
	Read(ctx context.Context, key string) ([]byte, error)
	Write(ctx context.Context, key string, data []byte) error
}

const timeout = 10 * time.Second

var (
	mu      sync.RWMutex
	backend Backend
	root    string
)

// Use routes every path under dataRoot to b. Use(nil, "") restores plain files.
func Use(b Backend, dataRoot string) {
	mu.Lock()
	defer mu.Unlock()
	backend = b
	root = filepath.Clean(dataRoot)
}

// Active reports whether a backend is installed, and for which root.
func Active() (bool, string) {
	mu.RLock()
	defer mu.RUnlock()
	return backend != nil, root
}

func route(path string) (Backend, string) {
	mu.RLock()
	defer mu.RUnlock()
	if backend == nil || root == "" || root == "." {
		return nil, ""
	}
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, ""
	}
	return backend, filepath.ToSlash(rel)
}

// ReadFile reads path. A missing state is reported so that os.IsNotExist and
// errors.Is(err, fs.ErrNotExist) both hold, as with os.ReadFile.
func ReadFile(path string) ([]byte, error) {
	b, key := route(path)
	if b == nil {
		return os.ReadFile(path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	data, err := b.Read(ctx, key)
	if err != nil {
		if isNotExist(err) {
			return nil, &fs.PathError{Op: "read", Path: path, Err: fs.ErrNotExist}
		}
		return nil, &fs.PathError{Op: "read", Path: path, Err: err}
	}
	return data, nil
}

// WriteFile replaces path with data. Plain files are written atomically
// (temp file + rename) after creating the parent directory.
func WriteFile(path string, data []byte, perm os.FileMode) error {
	b, key := route(path)
	if b == nil {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, data, perm); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := b.Write(ctx, key, data); err != nil {
		return &fs.PathError{Op: "write", Path: path, Err: err}
	}
	return nil
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err)
}
