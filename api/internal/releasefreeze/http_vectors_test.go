package releasefreeze

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// freezeHTTPVectorsSHA256 is the digest of testdata/freeze_http_vectors.json: raw
// responses with the verdict the language H of design §3.2 gives each. The file is
// generated in the infra repository (scripts/release/freeze_http_vectors.py) by a
// recogniser written from the productions; the Python reader is held to the same file.
const freezeHTTPVectorsSHA256 = "ea2f51577ddd33a1eda3b7ee4e53f401ed826ad00ab0d906e0d98e6ff96f4a3a"

type httpVector struct {
	Name  string `json:"name"`
	Hex   string `json:"hex"`
	Clear bool   `json:"clear"`
}

// memConn stands in for the connection when a response is read from memory.
type memConn struct{ net.Conn }

func (memConn) SetDeadline(time.Time) error { return nil }
func (memConn) Close() error                { return nil }

// readFromMemory runs the reader of the gate over raw response bytes.
func readFromMemory(raw []byte) (clear bool) {
	defer func() {
		if recover() != nil {
			clear = false
		}
	}()
	d, err := readHTTP(bufio.NewReader(bytes.NewReader(raw)), memConn{}, time.Now().Add(time.Hour), "t", Gate{}, context.Background(), nil)
	return err == nil && d.Verdict == VerdictClear
}

func loadHTTPVectors(t *testing.T) ([]httpVector, []byte, string) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	dir := filepath.Dir(file)
	raw, err := os.ReadFile(filepath.Join(dir, "testdata", "freeze_http_vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != freezeHTTPVectorsSHA256 {
		t.Fatalf("vector digest %s, constant %s", got, freezeHTTPVectorsSHA256)
	}
	var parsed struct {
		Cases []httpVector `json:"cases"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return parsed.Cases, raw, dir
}

// TestHTTPVectors holds the Go reader to the verdict H gives every raw response.
func TestHTTPVectors(t *testing.T) {
	cases, _, _ := loadHTTPVectors(t)
	if len(cases) < 280 {
		t.Fatalf("http vector cases %d", len(cases))
	}
	accepted := 0
	for _, c := range cases {
		raw, err := hex.DecodeString(c.Hex)
		if err != nil {
			t.Fatalf("%s: %v", c.Name, err)
		}
		if got := readFromMemory(raw); got != c.Clear {
			t.Errorf("%s: clear %v, want %v", c.Name, got, c.Clear)
		}
		if c.Clear {
			accepted++
		}
	}
	if accepted < 40 {
		t.Fatalf("only %d responses are expected clear: the file no longer exercises acceptance", accepted)
	}
	t.Logf("http vector cases %d, clear %d", len(cases), accepted)
}

func TestHTTPVectorFileMatchesSibling(t *testing.T) {
	_, raw, dir := loadHTTPVectors(t)
	sibling := filepath.Join(dir, "..", "..", "..", "..", "bifrost-trade-infra", "scripts", "release", "freeze_http_vectors.json")
	other, err := os.ReadFile(sibling)
	if err != nil {
		if os.Getenv("BIFROST_CI") != "" {
			t.Fatalf("sibling http vectors missing: %v", err)
		}
		t.Skipf("sibling http vectors absent: %v", err)
	}
	if !bytes.Equal(other, raw) {
		t.Fatal("platform and infra freeze_http_vectors.json differ")
	}
}
