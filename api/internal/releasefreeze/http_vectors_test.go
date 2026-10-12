package releasefreeze

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// freezeHTTPVectorsSHA256 is the digest of testdata/freeze_http_vectors.json: raw
// responses with the verdict the language H of design §3.2 gives each. The file is
// generated in the infra repository (scripts/release/freeze_http_vectors.py) by a
// recogniser written from the productions; the Python reader is held to the same file.
const freezeHTTPVectorsSHA256 = "819e95a595677d4114368528a7a8c1c4a9fd3a27bf07946f461c6ae8cd1b5b36"

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

type timingStep struct {
	AtMs int    `json:"at_ms"`
	Hex  string `json:"hex"`
}

type timingVector struct {
	Name     string       `json:"name"`
	BudgetMs int          `json:"budget_ms"`
	Steps    []timingStep `json:"steps"`
	Clear    bool         `json:"clear"`
}

type byteClass struct {
	Name            string   `json:"name"`
	Base            string   `json:"base"`
	Positions       []int    `json:"positions"`
	Replace         []string `json:"replace"`
	InsertPositions []int    `json:"insert_positions"`
	Insert          []string `json:"insert"`
}

type httpVectorFile struct {
	Cases       []httpVector   `json:"cases"`
	Timing      []timingVector `json:"timing"`
	ByteClasses []byteClass    `json:"byte_classes"`
}

func loadHTTPVectorFile(t *testing.T) httpVectorFile {
	t.Helper()
	_, raw, _ := loadHTTPVectors(t)
	var parsed httpVectorFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return parsed
}

// TestHTTPByteClasses puts every byte value at every position of a valid response
// that is not strictly inside the body data. Replace overwrites byte k; insert
// places a byte before k. The reader has to agree with the recogniser on all 256.
func TestHTTPByteClasses(t *testing.T) {
	file := loadHTTPVectorFile(t)
	n := 0
	var detail []string
	for _, base := range file.ByteClasses {
		raw, err := hex.DecodeString(base.Base)
		if err != nil {
			t.Fatalf("%s: base %v", base.Name, err)
		}
		if len(base.Positions) != len(base.Replace) || len(base.InsertPositions) != len(base.Insert) {
			t.Fatalf("%s: mask count", base.Name)
		}
		if len(base.Positions) == 0 || base.Positions[0] != 0 {
			t.Fatalf("%s: the first byte of the status line is not a position", base.Name)
		}
		n += len(base.Positions) + len(base.InsertPositions)
		detail = append(detail, fmt.Sprintf("%s replace %d insert %d", base.Name, len(base.Positions), len(base.InsertPositions)))
		compare := func(mode string, positions []int, masks []string, insert bool) {
			t.Helper()
			for i, k := range positions {
				mask, err := hex.DecodeString(masks[i])
				if err != nil || len(mask) != 32 {
					t.Fatalf("%s %s %d: bad mask", base.Name, mode, k)
				}
				for v := 0; v < 256; v++ {
					trial := append(append([]byte{}, raw[:k]...), byte(v))
					if insert {
						trial = append(trial, raw[k:]...)
					} else {
						trial = append(trial, raw[k+1:]...)
					}
					want := mask[v>>3]>>(v&7)&1 == 1
					if got := readFromMemory(trial); got != want {
						t.Errorf("%s %s %d 0x%02x: clear %v, want %v", base.Name, mode, k, v, got, want)
					}
				}
			}
		}
		compare("replace", base.Positions, base.Replace, false)
		compare("insert", base.InsertPositions, base.Insert, true)
	}
	if n < 2654 {
		t.Fatalf("byte-class positions %d", n)
	}
	t.Logf("byte-class positions %d; %s", n, strings.Join(detail, "; "))
}

// pauseFill returns bytes that are already available, then sleeps. The connection
// stub honours no deadline, so only the check after a clear verdict can refuse it.
type pauseFill struct{ *bytes.Reader }

func (p pauseFill) Read(b []byte) (int, error) {
	n, err := p.Reader.Read(b)
	time.Sleep(120 * time.Millisecond)
	return n, err
}

// TestSecondDeadlineHold is the case the per-read deadline does not see: every byte
// is available at once, and the read itself is held past the deadline.
func TestSecondDeadlineHold(t *testing.T) {
	body := EmptyListBody()
	raw := append([]byte("HTTP/1.1 200\r\nContent-Length: "+strconv.Itoa(len(body))+"\r\n\r\n"), body...)
	fast, err := readHTTP(bufio.NewReader(bytes.NewReader(raw)), memConn{}, time.Now().Add(100*time.Millisecond), "t", Gate{}, context.Background(), nil)
	if err != nil || fast.Verdict != VerdictClear {
		t.Fatalf("undelayed: clear %v err %v", fast.Verdict, err)
	}
	start := time.Now()
	slow, err := readHTTP(bufio.NewReader(pauseFill{bytes.NewReader(raw)}), memConn{}, start.Add(100*time.Millisecond), "t", Gate{}, context.Background(), nil)
	if err == nil && slow.Verdict == VerdictClear {
		t.Fatalf("clear after the read slept past the deadline (%v)", time.Since(start))
	}
}

// TestHTTPTiming delivers the same bytes at different moments over an in-memory
// connection that honours deadlines: one deadline to the last byte H needs.
func TestHTTPTiming(t *testing.T) {
	file := loadHTTPVectorFile(t)
	if len(file.Timing) < 12 {
		t.Fatalf("timing cases %d", len(file.Timing))
	}
	for _, c := range file.Timing {
		client, server := net.Pipe()
		start := time.Now()
		done := make(chan struct{})
		go func(steps []timingStep) {
			defer close(done)
			for _, st := range steps {
				part, err := hex.DecodeString(st.Hex)
				if err != nil {
					return
				}
				time.Sleep(time.Until(start.Add(time.Duration(st.AtMs) * time.Millisecond)))
				if _, err := server.Write(part); err != nil {
					return
				}
			}
		}(c.Steps)
		budget := time.Duration(c.BudgetMs) * time.Millisecond
		deadline := start.Add(budget)
		d, err := readHTTP(bufio.NewReader(client), client, deadline, "t", Gate{}, context.Background(), nil)
		took := time.Since(start)
		got := err == nil && d.Verdict == VerdictClear
		_ = client.Close()
		_ = server.Close()
		<-done
		if got != c.Clear {
			t.Errorf("%s: clear %v after %v, want %v", c.Name, got, took, c.Clear)
		} else if took > budget+250*time.Millisecond {
			t.Errorf("%s: answered only after %v", c.Name, took)
		}
	}
	t.Logf("timing cases %d", len(file.Timing))
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
