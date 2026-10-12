package releasefreeze

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"testing"
)

// freezeVectorsSHA256 is the digest of testdata/freeze_vectors.json.
// The Python copy in bifrost-trade-infra carries the same constant.
const freezeVectorsSHA256 = "a5f19e65ba542bb7de6180462032991c10021467b6642a24dc88ba8ff4f6a3ee"

type vectorFile struct {
	Tokens   []string     `json:"tokens"`
	Patterns []string     `json:"patterns"`
	Cases    []vectorCase `json:"cases"`
}

type vectorCase struct {
	Name       string   `json:"name"`
	Status     int      `json:"status"`
	Body       string   `json:"body"`
	BodyBase64 string   `json:"body_base64"`
	Transport  string   `json:"transport"`
	Verdict    string   `json:"verdict"`
	FirstLine  string   `json:"first_line"`
	Lines      []string `json:"lines"`
	Records    []struct {
		Name   string `json:"name"`
		UID    string `json:"uid"`
		Who    string `json:"who"`
		Reason string `json:"reason"`
		At     string `json:"at"`
		Via    string `json:"via"`
	} `json:"records"`
}

func loadVectors(t *testing.T) (vectorFile, []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "freeze_vectors.json"))
	if err != nil {
		t.Fatalf("read vectors: %v", err)
	}
	sum := sha256.Sum256(raw)
	got := hex.EncodeToString(sum[:])
	if got != freezeVectorsSHA256 {
		t.Fatalf("vector digest %s, constant %s", got, freezeVectorsSHA256)
	}
	var file vectorFile
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatalf("parse vectors: %v", err)
	}
	return file, raw
}

func TestVectors(t *testing.T) {
	file, _ := loadVectors(t)
	if len(file.Tokens) != len(ReasonTokens) {
		t.Fatalf("token list length %d, file %d", len(ReasonTokens), len(file.Tokens))
	}
	for i, tok := range file.Tokens {
		if ReasonTokens[i] != tok {
			t.Fatalf("token %d: code %q file %q", i, ReasonTokens[i], tok)
		}
	}
	patterns := make([]*regexp.Regexp, len(file.Patterns))
	for i, p := range file.Patterns {
		patterns[i] = regexp.MustCompile(p)
	}
	allowed := map[string]bool{}
	for _, tok := range file.Tokens {
		allowed[tok] = true
	}
	reasonOK := func(reason string) bool {
		if allowed[reason] {
			return true
		}
		for _, p := range patterns {
			if p.MatchString(reason) {
				return true
			}
		}
		return false
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
			if got.Verdict != c.Verdict {
				t.Fatalf("verdict %s, want %s (%s)", got.Verdict, c.Verdict, got.Lines)
			}
			if len(got.Lines) == 0 || got.Lines[0] != c.FirstLine {
				t.Fatalf("first line %q, want %q", first(got.Lines), c.FirstLine)
			}
			if c.Verdict == VerdictUnreadable && !reasonOK(got.Reason) {
				t.Fatalf("reason %q is outside the closed list", got.Reason)
			}
			if c.Lines != nil {
				if len(got.Lines) != len(c.Lines) {
					t.Fatalf("lines %q, want %q", got.Lines, c.Lines)
				}
				for i := range c.Lines {
					if got.Lines[i] != c.Lines[i] {
						t.Fatalf("line %d %q, want %q", i, got.Lines[i], c.Lines[i])
					}
				}
			}
			if c.Records != nil {
				if len(got.Records) != len(c.Records) {
					t.Fatalf("records %d, want %d", len(got.Records), len(c.Records))
				}
				for i, want := range c.Records {
					gotR := got.Records[i]
					if gotR.Name != want.Name || gotR.UID != want.UID || gotR.Who != want.Who ||
						gotR.Reason != want.Reason || gotR.At != want.At || gotR.Via != want.Via {
						t.Fatalf("record %+v, want %+v", gotR, want)
					}
				}
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

func first(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}
