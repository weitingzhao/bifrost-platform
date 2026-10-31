package s5check

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var digestSHA = regexp.MustCompile(`^(sha256:)?[0-9a-f]{64}$`)

const maxReadBytes = 128 << 20
const maxBlobBytes = 8 << 20

// GitTreeReader reads a local repository's immutable object database. It never
// checks out files, updates refs, fetches, invokes filters or executes hooks.
// Bare marks Directory as the Git directory itself (the CI's read-only object
// store, owned by the fetch task's user), not a worktree.
type GitTreeReader struct {
	Directory string
	Bare      bool
}

var s5Ref = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

func validPath(name string) bool {
	return name != "" && name != "." && name == path.Clean(name) && !path.IsAbs(name) &&
		!strings.HasPrefix(name, "../") && !strings.ContainsAny(name, "\\\x00\r\n") && utf8.ValidString(name)
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		return 0, errors.New("checker subprocess output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func (g GitTreeReader) run(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	if !filepath.IsAbs(g.Directory) {
		return nil, errors.New("trusted repository path must be absolute")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	base := []string{"--no-replace-objects", "-c", "core.hooksPath=/dev/null", "-c", "core.attributesFile=/dev/null",
		"-c", "core.fsmonitor=false", "-c", "credential.helper=", "-c", "diff.external=", "-c", "core.quotePath=false",
		"-c", "submodule.recurse=false", "-c", "protocol.allow=never"}
	if g.Bare {
		base = append([]string{"--git-dir=" + g.Directory, "-c", "safe.directory=" + g.Directory}, base...)
	}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	cmd.Dir = g.Directory
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=/dev/null", "LC_ALL=C", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_ATTR_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_LITERAL_PATHSPECS=1"}
	cmd.Stdin = bytes.NewReader(input)
	out := &boundedBuffer{limit: maxReadBytes}
	stderr := &boundedBuffer{limit: 8192}
	cmd.Stdout = out
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("read-only git %s failed: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}

func (g GitTreeReader) ReadTree(ctx context.Context, revision string) (TreeSnapshot, error) {
	out := TreeSnapshot{Files: map[string]TreeEntry{}}
	if !fullSHA.MatchString(revision) {
		return out, errors.New("revision must be a full lowercase Git commit SHA")
	}
	meta, err := g.run(ctx, nil, "show", "-s", "--format=%H%n%T%n%P", revision, "--")
	if err != nil {
		return out, err
	}
	lines := strings.Split(strings.TrimSuffix(string(meta), "\n"), "\n")
	if len(lines) != 3 || lines[0] != revision || !fullSHA.MatchString(lines[1]) {
		return out, errors.New("invalid commit identity")
	}
	out.Commit, out.Tree, out.Parents = lines[0], lines[1], strings.Fields(lines[2])
	for _, p := range out.Parents {
		if !fullSHA.MatchString(p) {
			return out, errors.New("invalid parent identity")
		}
	}
	raw, err := g.run(ctx, nil, "ls-tree", "-r", "-z", "--full-tree", revision, "--")
	if err != nil {
		return out, err
	}
	blobPaths := map[string][]string{}
	for _, entry := range bytes.Split(raw, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		pair := bytes.SplitN(entry, []byte{'\t'}, 2)
		if len(pair) != 2 {
			return out, errors.New("malformed tree entry")
		}
		fields := strings.Fields(string(pair[0]))
		name := string(pair[1])
		if len(fields) != 3 || !validPath(name) || !fullSHA.MatchString(fields[2]) {
			return out, errors.New("invalid tree entry")
		}
		if _, exists := out.Files[name]; exists {
			return out, errors.New("duplicate tree path")
		}
		out.Files[name] = TreeEntry{Mode: fields[0], Blob: fields[2]}
		if fields[1] == "blob" && scannerInput(name) {
			blobPaths[fields[2]] = append(blobPaths[fields[2]], name)
		}
	}
	hashes := make([]string, 0, len(blobPaths))
	for hash := range blobPaths {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	if len(hashes) == 0 {
		return out, nil
	}
	batch, err := g.run(ctx, []byte(strings.Join(hashes, "\n")+"\n"), "cat-file", "--batch")
	if err != nil {
		return out, err
	}
	r := bufio.NewReader(bytes.NewReader(batch))
	for _, hash := range hashes {
		header, err := r.ReadString('\n')
		if err != nil {
			return out, err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[0] != hash || fields[1] != "blob" {
			return out, errors.New("invalid batch blob identity")
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size > maxBlobBytes {
			return out, errors.New("checker input blob exceeds limit")
		}
		body := make([]byte, size)
		if _, err = io.ReadFull(r, body); err != nil {
			return out, err
		}
		if sep, err := r.ReadByte(); err != nil || sep != '\n' {
			return out, errors.New("invalid batch separator")
		}
		binary := !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0
		for _, name := range blobPaths[hash] {
			entry := out.Files[name]
			entry.Binary = binary
			if !binary {
				entry.Content = string(body)
			}
			out.Files[name] = entry
		}
	}
	if _, err = r.Peek(1); err != io.EOF {
		return out, errors.New("trailing batch data")
	}
	return out, nil
}

// ResolveCommitRef resolves refs/s5/<name> to a full commit SHA. The CI binds
// the fetched refs to the key before reading any object by SHA.
func (g GitTreeReader) ResolveCommitRef(ctx context.Context, name string) (string, error) {
	if !s5Ref.MatchString(name) {
		return "", errors.New("invalid S5 ref name")
	}
	out, err := g.run(ctx, nil, "rev-parse", "--verify", "--quiet", "--end-of-options", "refs/s5/"+name+"^{commit}")
	if err != nil {
		return "", err
	}
	sha := strings.TrimSuffix(string(out), "\n")
	if !fullSHA.MatchString(sha) {
		return "", errors.New("ref does not name a full commit")
	}
	return sha, nil
}

func scannerInput(name string) bool {
	switch path.Ext(name) {
	case ".ts", ".tsx", ".json", ".css":
		return true
	}
	return false
}

func (g GitTreeReader) ReadPatch(ctx context.Context, before, after string) (Patch, error) {
	var out Patch
	if !fullSHA.MatchString(before) || !fullSHA.MatchString(after) {
		return out, errors.New("invalid patch revisions")
	}
	raw, err := g.run(ctx, nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--full-index", "--binary", before, after, "--")
	if err != nil {
		return out, err
	}
	id, err := g.run(ctx, raw, "patch-id", "--stable")
	if err != nil {
		return out, err
	}
	fields := strings.Fields(string(id))
	if len(fields) != 2 || !fullSHA.MatchString(fields[0]) {
		return out, errors.New("patch is empty or has no stable identity")
	}
	out.Bytes, out.StableID = raw, fields[0]
	return out, nil
}
