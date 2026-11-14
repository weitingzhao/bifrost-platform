package landinggit

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
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
		":100644 000000 " + a + " " + zero + " D\x00scripts/x.sh\x00" +
		":100755 100755 " + a + " " + b + " M\x00scripts/update.sh\x00"
	files, err := floorRaw(raw, read)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(blobs[a]))
	if len(files) != 4 || files[0].Path != "k8s/a.yaml" || files[0].Kind != "M" || files[0].Mode != "100644" || files[0].SHA256 != hex.EncodeToString(sum[:]) || files[0].Secret {
		t.Fatalf("modified: %+v", files)
	}
	if !files[1].Secret || files[1].Kind != "A" {
		t.Fatalf("secret: %+v", files[1])
	}
	if files[2].Kind != "D" || files[2].SHA256 != "" || files[2].Mode != "" {
		t.Fatalf("deleted: %+v", files[2])
	}
	if files[3].OldMode != "100755" || files[3].Mode != "100755" {
		t.Fatalf("existing executable: %+v", files[3])
	}
	if _, err := floorRaw(":100644 100644 x y M\x00", read); err == nil {
		t.Fatal("malformed accepted")
	}
	if files, err := floorRaw("", read); err != nil || len(files) != 0 {
		t.Fatal("empty", files, err)
	}
}
