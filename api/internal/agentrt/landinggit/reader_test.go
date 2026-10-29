package landinggit

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Owner 2026-10-29: platform-api reads a grant's material with the Gitea
// reader, never the lander. The reader must refuse every write before any
// network, while the lander constructor (the control) still builds a writer.
// Serial: t.Setenv.
func TestReaderPushesNothing(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{"username", "password"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(ReaderDirEnv, "relative")
	if _, err := ReaderFromEnv(); err == nil {
		t.Fatal("a relative reader directory was accepted")
	}
	t.Setenv(ReaderDirEnv, dir)
	g, err := ReaderFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if g.lander || g.usernameFile != filepath.Join(dir, "username") || g.passwordFile != filepath.Join(dir, "password") {
		t.Fatalf("reader identity %+v", g)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sha := strings.Repeat("a", 40)
	w := &Workspace{git: g, dir: t.TempDir()}
	for _, ref := range []string{"refs/heads/main", "refs/heads/land/prod/t_00000000000000000000000001/1", "refs/heads/land/stg/t_00000000000000000000000001/1"} {
		if err := w.Push(ctx, ref, sha); err == nil || !strings.Contains(err.Error(), "outside landing scope") {
			t.Fatalf("reader push to %s: %v", ref, err)
		}
		if err := g.Delete(ctx, ref, sha); err == nil || !strings.Contains(err.Error(), "outside landing scope") {
			t.Fatalf("reader delete of %s: %v", ref, err)
		}
	}
	if _, err := g.Ref(ctx, "refs/heads/land/prod/t_00000000000000000000000001/1"); err == nil {
		t.Fatal("reader read a land/prod branch")
	}
	if out := w.PushMain(ctx, sha, strings.Repeat("b", 40), time.Now().Add(time.Hour)); out != PushNotSent {
		t.Fatalf("reader main push %v", out)
	}
	// Control: the lander is the writer for land/prod and main.
	t.Setenv(LanderUsernameFileEnv, filepath.Join(dir, "username"))
	t.Setenv(LanderPasswordFileEnv, filepath.Join(dir, "password"))
	l, err := LanderFromEnv("prod")
	if err != nil || !l.lander || !l.inScope("refs/heads/land/prod/t_00000000000000000000000001/1") {
		t.Fatalf("lander control %+v %v", l, err)
	}
	if g.inScope("refs/heads/land/prod/t_00000000000000000000000001/1") {
		t.Fatal("reader in landing scope")
	}
	// The lander keeps exact bytes: a trailing newline is refused there.
	if _, err := l.header(); err == nil {
		t.Fatal("lander accepted a trailing newline")
	}
}

// The reader parses its files as delivery/gitea_reader.go does: at most 4096
// bytes, surrounding whitespace dropped, one line, no NUL (reviews 713 R-01,
// 717 B-1). The lander keeps exact bytes (control).
func TestReaderCredentialContract(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	user := filepath.Join(dir, "username")
	if err := os.WriteFile(user, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, password string
		ok             bool
	}{
		{"lf", "x\n", true},
		{"crlf", "x\r\n", true},
		{"spaces", "  x  ", true},
		{"4096 bytes", strings.Repeat("p", 4096), true},
		{"4097 bytes", strings.Repeat("p", 4097), false},
		{"nul", "x\x00y\n", false},
		{"two lines", "x\ny\n", false},
		{"blank", "\n", false},
	}
	for i, c := range cases {
		pass := filepath.Join(dir, fmt.Sprintf("password-%d", i))
		if err := os.WriteFile(pass, []byte(c.password), 0o600); err != nil {
			t.Fatal(err)
		}
		g := &Git{reader: true, scope: noRef, usernameFile: user, passwordFile: pass}
		h, err := g.header()
		if (err == nil) != c.ok {
			t.Fatalf("reader %s: header %q err %v, want ok=%v", c.name, h, err, c.ok)
		}
		if c.ok && h != "Authorization: Basic "+base64.StdEncoding.EncodeToString([]byte("x:"+strings.TrimSpace(c.password))) {
			t.Fatalf("reader %s: header %q", c.name, h)
		}
	}
	lander := &Git{lander: true, usernameFile: user, passwordFile: user}
	if _, err := lander.header(); err == nil {
		t.Fatal("lander accepted a trailing newline")
	}
}

// Every identity refuses an empty ref before any network (review 713 N-01).
// Its own test, so a failure here cannot hide the credential contract
// (review 717 B-2). The lander is the control: it still writes its scope.
func TestEmptyRefIsOutsideEveryScope(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sha := strings.Repeat("a", 40)
	reader := &Git{reader: true, scope: noRef}
	lander := &Git{lander: true, scope: liveRef}
	for name, g := range map[string]*Git{"reader": reader, "lander": lander} {
		w := &Workspace{git: g, dir: t.TempDir()}
		if err := w.Push(ctx, "", sha); err == nil || !strings.Contains(err.Error(), "outside landing scope") {
			t.Fatalf("%s push to an empty ref: %v", name, err)
		}
		if err := g.Delete(ctx, "", sha); err == nil || !strings.Contains(err.Error(), "outside landing scope") {
			t.Fatalf("%s delete of an empty ref: %v", name, err)
		}
		if _, err := g.Ref(ctx, ""); err == nil || !strings.Contains(err.Error(), "outside landing scope") {
			t.Fatalf("%s read of an empty ref: %v", name, err)
		}
	}
	if !lander.inScope("refs/heads/land/prod/t_00000000000000000000000001/1") {
		t.Fatal("lander control lost its land/prod scope")
	}
}
