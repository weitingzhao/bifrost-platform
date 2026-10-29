package landinggit

import (
	"context"
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
}
