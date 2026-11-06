package landinggit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFloorRaw(t *testing.T) {
	t.Parallel()
	a, b := strings.Repeat("a", 40), strings.Repeat("b", 40)
	blobs := map[string]string{a: "kind: ConfigMap\n", b: "apiVersion: v1\nkind: Secret\n"}
	read := func(sha string) ([]byte, error) {
		v, ok := blobs[sha]
		if !ok {
			return nil, errors.New("no blob")
		}
		return []byte(v), nil
	}
	zero := strings.Repeat("0", 40)
	raw := ":100644 100644 " + zero + " " + a + " M\x00k8s/a.yaml\x00" +
		":000000 100644 " + zero + " " + b + " A\x00k8s/s.yaml\x00" +
		":100644 000000 " + a + " " + zero + " D\x00scripts/x.sh\x00"
	files, err := floorRaw(raw, read)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(blobs[a]))
	if len(files) != 3 || files[0].Path != "k8s/a.yaml" || files[0].Kind != "M" || files[0].Mode != "100644" || files[0].SHA256 != hex.EncodeToString(sum[:]) || files[0].Secret {
		t.Fatalf("modified: %+v", files)
	}
	if !files[1].Secret || files[1].Kind != "A" {
		t.Fatalf("secret: %+v", files[1])
	}
	if files[2].Kind != "D" || files[2].SHA256 != "" || files[2].Mode != "" {
		t.Fatalf("deleted: %+v", files[2])
	}
	if _, err := floorRaw(":100644 100644 x y M\x00", read); err == nil {
		t.Fatal("malformed accepted")
	}
	if files, err := floorRaw("", read); err != nil || len(files) != 0 {
		t.Fatal("empty", files, err)
	}
}

// FloorFiles and Pusher against a real remote: R one commit on M0 adding a
// manifest and changing a script; the lander names its Gitea username.
func TestFloorFilesAndPusher(t *testing.T) {
	g, in, src := fixture(t)
	ctx := context.Background()
	repo := filepath.Join(filepath.Dir(src), "remote.git")
	gitTest(t, src, "checkout", "-q", "-b", "floor", in.M0)
	for name, content := range map[string]string{"k8s/app.yaml": "kind: ConfigMap\n", "scripts/x.sh": "echo floor\n"} {
		if e := os.MkdirAll(filepath.Join(src, filepath.Dir(name)), 0o700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(src, name), []byte(content), 0o600); e != nil {
			t.Fatal(e)
		}
		gitTest(t, src, "add", name)
	}
	gitTest(t, src, "commit", "-m", "floor")
	r := gitTest(t, src, "rev-parse", "HEAD")
	gitTest(t, src, "push", repo, r+":refs/heads/land/prod/"+testTask+"/1")
	tree, err := g.FloorFiles(ctx, in.M0, r)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("kind: ConfigMap\n"))
	if !tree.OnM0 || len(tree.Files) != 2 || tree.Files[0].Path != "k8s/app.yaml" || tree.Files[0].Kind != "A" || tree.Files[0].Mode != "100644" ||
		tree.Files[0].SHA256 != hex.EncodeToString(sum[:]) || tree.Files[1].Path != "scripts/x.sh" {
		t.Fatalf("tree: %+v", tree)
	}
	if other, err := g.FloorFiles(ctx, in.Base, r); err != nil || other.OnM0 || len(other.Files) != 0 {
		t.Fatalf("R is not on base: %+v %v", other, err)
	}
	lander := &Git{remote: g.remote, usernameFile: g.usernameFile, passwordFile: g.passwordFile, scope: liveRef, lander: true}
	w, err := lander.Open(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if name, err := w.Pusher(); err != nil || name != "drill-fixture" {
		t.Fatal("pusher", name, err)
	}
	drill, err := g.Open(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	defer drill.Close()
	if _, err := drill.Pusher(); err == nil {
		t.Fatal("the drill named a pusher")
	}
}
