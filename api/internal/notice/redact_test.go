package notice

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRedactVectors(t *testing.T) {
	raw, err := os.ReadFile("../questions/testdata/canonical/vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Redact []struct {
			Name    string   `json:"name"`
			Input   string   `json:"input"`
			Output  string   `json:"output"`
			Secrets []string `json:"secrets"`
		} `json:"redact"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Redact) < 9 {
		t.Fatalf("redact vectors = %d, want the adjacent-key cases", len(file.Redact))
	}
	for _, v := range file.Redact {
		t.Run(v.Name, func(t *testing.T) {
			old := redactAt8b3259a(v.Input)
			got := Redact(v.Input)
			if got != v.Output {
				t.Fatalf("Redact(%q) = %q, want %q", v.Input, got, v.Output)
			}
			for _, sec := range v.Secrets {
				if !strings.Contains(v.Input, sec) {
					t.Fatalf("vector does not contain %q", sec)
				}
				if strings.Contains(old, sec) {
					t.Fatalf("redactAt8b3259a kept %q in %q", sec, old)
				}
				if strings.Contains(got, sec) {
					t.Fatalf("Redact kept %q that main removed", sec)
				}
			}
		})
	}
}

// TestNonASCIICaseFold records where main's (?i) rules and the spelled-out
// classes disagree. U+212A and U+017F still count as K and S. When one of
// those runes is the first character of the match, main's \b does not see an
// ASCII word edge, so main keeps the secret and Redact removes it.
func TestNonASCIICaseFold(t *testing.T) {
	const secret = "s3cr3tvalue"
	for _, in := range []string{
		"to\u212Aen=" + secret,
		"x\u017Fecret=" + secret,
		"api_\u212Aey=" + secret,
	} {
		old, got := redactAt8b3259a(in), Redact(in)
		if strings.Contains(got, secret) || old != got {
			t.Fatalf("%q\n old %q\n new %q", in, old, got)
		}
	}
	for _, in := range []string{
		"\u017Fecret=" + secret,
		" \u017Fecret=" + secret,
	} {
		old, got := redactAt8b3259a(in), Redact(in)
		if !strings.Contains(old, secret) || strings.Contains(got, secret) {
			t.Fatalf("want main to keep and Redact to drop %q\n old %q\n new %q", in, old, got)
		}
	}
}

func TestTokenTailStillRedacts(t *testing.T) {
	gh := "ghp_" + strings.Repeat("a", 20)
	in := gh + "_tail"
	old, got := redactAt8b3259a(in), Redact(in)
	if strings.Contains(old, gh) {
		t.Fatalf("main kept %s in %q", gh, old)
	}
	if strings.Contains(got, gh) {
		t.Fatalf("Redact kept %s\n old %q\n new %q", gh, old, got)
	}
}
