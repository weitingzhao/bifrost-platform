package releasefreeze

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// freezeVectorsSHA256 is the digest of testdata/freeze_vectors.json.
// The Python copy carries the same constant. The old 68-case digest is retired.
const freezeVectorsSHA256 = "b20086845a61e9a0d92765472aa932ebbc517dd5c0f4d107e5ff959080c3bf72"

type vectorFile struct {
	Cases []vectorCase `json:"cases"`
}

type vectorCase struct {
	Name       string `json:"name"`
	Status     int    `json:"status"`
	Body       string `json:"body"`
	BodyBase64 string `json:"body_base64"`
	Transport  string `json:"transport"`
	Clear      bool   `json:"clear"`
	FirstLine  string `json:"first_line"`
}

func loadVectors(t *testing.T) (vectorFile, []byte) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "testdata", "freeze_vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != freezeVectorsSHA256 {
		t.Fatalf("vector digest %s, constant %s", got, freezeVectorsSHA256)
	}
	var parsed vectorFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return parsed, raw
}

func TestVectors(t *testing.T) {
	file, _ := loadVectors(t)
	if len(file.Cases) < 20 {
		t.Fatalf("vector cases %d", len(file.Cases))
	}
	t.Logf("vector cases %d", len(file.Cases))
	for _, c := range file.Cases {
		t.Run(c.Name, func(t *testing.T) {
			var got Decision
			if c.Transport != "" {
				got = DecideTransport(c.Transport)
			} else {
				body := []byte(c.Body)
				if c.BodyBase64 != "" {
					var err error
					body, err = base64.StdEncoding.DecodeString(c.BodyBase64)
					if err != nil {
						t.Fatalf("base64: %v", err)
					}
				}
				got = DecideHTTP(c.Status, body)
			}
			clear := got.Verdict == VerdictClear
			if clear != c.Clear {
				t.Fatalf("clear %v, want %v (%s)", clear, c.Clear, got.Lines)
			}
			if c.Clear && (len(got.Lines) == 0 || got.Lines[0] != c.FirstLine) {
				t.Fatalf("first line %q, want %q", firstLine(got), c.FirstLine)
			}
		})
	}
}

func TestVectorFileMatchesSibling(t *testing.T) {
	_, raw := loadVectors(t)
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	sibling := filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "bifrost-trade-infra", "scripts", "release", "freeze_vectors.json")
	other, err := os.ReadFile(sibling)
	if err != nil {
		if os.Getenv("BIFROST_CI") != "" {
			t.Fatalf("sibling vectors missing: %v", err)
		}
		t.Skipf("sibling vectors absent: %v", err)
	}
	if string(other) != string(raw) {
		t.Fatal("platform and infra freeze_vectors.json differ")
	}
}
