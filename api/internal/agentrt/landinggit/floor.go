package landinggit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// FloorFiles recomputes M0..R for a floor grant (approve_floor_landing): R
// is fetched by its SHA into a temporary clone that is removed after, its
// parents read, and every file M0..R changes listed (no rename detection)
// with its change kind, mode, the sha256 of its bytes at R and whether it
// declares a Secret. It pushes and writes nothing; every failure is an
// error, never an empty tree.
func (g *Git) FloorFiles(ctx context.Context, m0, r string) (landing.FloorTree, error) {
	var out landing.FloorTree
	if !fullSHA.MatchString(m0) || !fullSHA.MatchString(r) {
		return out, errors.New("invalid M0 or R")
	}
	dir, err := os.MkdirTemp("", "agentrt-landing-floor-")
	if err != nil {
		return out, err
	}
	defer os.RemoveAll(dir)
	if _, err = g.run(ctx, dir, false, nil, nil, "init", "--template=", "."); err != nil {
		return out, err
	}
	if _, err = g.run(ctx, dir, false, nil, nil, "remote", "add", "origin", g.remote); err != nil {
		return out, err
	}
	if _, err = g.run(ctx, dir, true, nil, nil, "fetch", "--no-tags", "--no-recurse-submodules", "origin", r); err != nil {
		return out, err
	}
	parents, err := g.run(ctx, dir, false, nil, nil, "rev-list", "--parents", "-n", "1", r)
	if err != nil {
		return out, err
	}
	p := strings.Fields(parents)
	out.OnM0 = len(p) == 2 && p[0] == r && p[1] == m0
	if !out.OnM0 {
		return out, nil
	}
	raw, err := g.run(ctx, dir, false, nil, nil, "diff", "--raw", "-z", "--no-abbrev", "--no-renames", m0, r, "--")
	if err != nil {
		return out, err
	}
	out.Files, err = floorRaw(raw, func(blob string) ([]byte, error) {
		b, err := g.run(ctx, dir, false, nil, nil, "cat-file", "blob", blob)
		return []byte(b), err
	})
	return out, err
}

// floorRaw reads `git diff --raw -z --no-renames`: per file ":<old mode>
// <new mode> <old sha> <new sha> <status>\0<path>\0". blob reads a blob's
// bytes by its SHA.
func floorRaw(raw string, blob func(string) ([]byte, error)) ([]landing.FloorFile, error) {
	files := []landing.FloorFile{}
	if raw == "" {
		return files, nil
	}
	fields := strings.Split(strings.TrimSuffix(raw, "\x00"), "\x00")
	if len(fields)%2 != 0 {
		return nil, errors.New("malformed raw diff")
	}
	for i := 0; i < len(fields); i += 2 {
		meta := strings.Fields(fields[i])
		if len(meta) != 5 || !strings.HasPrefix(meta[0], ":") || fields[i+1] == "" {
			return nil, errors.New("malformed raw diff")
		}
		f := landing.FloorFile{Path: fields[i+1], Kind: meta[4][:1], OldMode: strings.TrimPrefix(meta[0], ":"), Mode: meta[1]}
		if f.Kind == "D" {
			f.Mode = ""
		} else {
			if !fullSHA.MatchString(meta[3]) {
				return nil, errors.New("malformed raw diff")
			}
			b, err := blob(meta[3])
			if err != nil {
				return nil, err
			}
			sum := sha256.Sum256(b)
			f.SHA256 = hex.EncodeToString(sum[:])
			f.Secret = rules.InfraSecretManifest(f.Path, string(b))
		}
		files = append(files, f)
	}
	return files, nil
}
