// Package unitplan computes, for one commit of a repository, the units CI
// tests and the content key of each (CI design v4 §3.2, landing stage 2 block
// 2-B). It is pure: it reads a commit's tree through Tree and decides
// nothing. It runs no go command: a Go module's packages, files and imports
// are read with go/build over the tree.
package unitplan

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

// Entry is one path of a commit's tree: a file (git blob) or a directory
// (git tree), with its git object id.
type Entry struct {
	Path string
	Dir  bool
	ID   string
}

// Tree is the tree of one commit: every path under it by name, and the bytes
// of a file. Paths are relative to the repository root, slash separated.
type Tree struct {
	// RootID is the git tree id of the commit's root tree.
	RootID   string
	entries  map[string]Entry
	children map[string][]string // directory -> names directly under it, sorted
	read     func(id string) ([]byte, error)
}

// NewTree builds a Tree from the entries of a recursive listing (files and
// directories; git ls-tree -r -t) and a reader of a blob by its id.
func NewTree(rootID string, entries []Entry, read func(id string) ([]byte, error)) (*Tree, error) {
	t := &Tree{RootID: rootID, entries: map[string]Entry{"": {Path: "", Dir: true, ID: rootID}}, children: map[string][]string{}, read: read}
	for _, e := range entries {
		if e.Path == "" || path.Clean(e.Path) != e.Path || strings.HasPrefix(e.Path, "/") || strings.HasPrefix(e.Path, "../") {
			return nil, fmt.Errorf("unitplan: invalid tree path %q", e.Path)
		}
		if _, twice := t.entries[e.Path]; twice {
			return nil, fmt.Errorf("unitplan: tree path %q twice", e.Path)
		}
		t.entries[e.Path] = e
	}
	for p := range t.entries {
		if p == "" {
			continue
		}
		dir := path.Dir(p)
		if dir == "." {
			dir = ""
		}
		if parent, ok := t.entries[dir]; !ok || !parent.Dir {
			return nil, fmt.Errorf("unitplan: tree path %q has no directory %q", p, dir)
		}
		t.children[dir] = append(t.children[dir], path.Base(p))
	}
	for dir := range t.children {
		sort.Strings(t.children[dir])
	}
	return t, nil
}

// Lookup is the entry at p ("" and "." are the root).
func (t *Tree) Lookup(p string) (Entry, bool) {
	if p = strings.Trim(p, "/"); p == "." {
		p = ""
	}
	e, ok := t.entries[p]
	return e, ok
}

// Names lists the names directly under a directory, sorted.
func (t *Tree) Names(dir string) []string { return t.children[strings.Trim(dir, "/")] }

// ReadFile is the bytes of the file at p.
func (t *Tree) ReadFile(p string) ([]byte, error) {
	e, ok := t.Lookup(p)
	if !ok || e.Dir {
		return nil, fmt.Errorf("unitplan: %q is not a file of this tree: %w", p, fs.ErrNotExist)
	}
	if t.read == nil {
		return nil, errors.New("unitplan: this tree has no blob reader")
	}
	return t.read(e.ID)
}

// Walk lists every file under dir (dir itself when it is a file), sorted.
func (t *Tree) Walk(dir string) []Entry {
	dir = strings.Trim(dir, "/")
	root, ok := t.Lookup(dir)
	if !ok {
		return nil
	}
	if !root.Dir {
		return []Entry{root}
	}
	var out []Entry
	var walk func(d string)
	walk = func(d string) {
		for _, name := range t.children[d] {
			p := name
			if d != "" {
				p = d + "/" + name
			}
			if e := t.entries[p]; e.Dir {
				walk(p)
			} else {
				out = append(out, e)
			}
		}
	}
	walk(dir)
	return out
}

// fileInfo is a tree entry as go/build wants to see a directory entry.
type fileInfo struct {
	name string
	dir  bool
}

func (f fileInfo) Name() string { return f.name }
func (f fileInfo) Size() int64  { return 0 }
func (f fileInfo) Mode() fs.FileMode {
	if f.dir {
		return fs.ModeDir | 0o755
	}
	return 0o644
}
func (f fileInfo) ModTime() time.Time { return time.Time{} }
func (f fileInfo) IsDir() bool        { return f.dir }
func (f fileInfo) Sys() any           { return nil }
