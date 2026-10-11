package notice

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

// redactAtMain is Redact at main 8b3259a (api/internal/approvalnotify/redact.go).
// The differential test uses it to show the new rules still remove every ASCII
// secret that implementation removed.
func redactAtMain(s string) string {
	for _, r := range mainRedactions {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

var mainRedactions = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`), "[redacted private key]"},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "$1 [redacted]"},
	{regexp.MustCompile(`(?i)\b([A-Za-z0-9_.-]*(?:password|passwd|pwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential)[A-Za-z0-9_.-]*)(["']?\s*[:=]\s*["']?)[^\s"',;]+`), "$1$2[redacted]"},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "[redacted jwt]"},
	{regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`), "[redacted aws key]"},
	{regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9]{20,}|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,})`), "[redacted token]"},
	{regexp.MustCompile(`://([^/\s:@]+):([^@\s/]+)@`), "://$1:[redacted]@"},
}

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
			old := redactAtMain(v.Input)
			got := Redact(v.Input)
			if got != v.Output {
				t.Fatalf("Redact(%q) = %q, want %q", v.Input, got, v.Output)
			}
			for _, sec := range v.Secrets {
				if !strings.Contains(v.Input, sec) {
					t.Fatalf("vector does not contain %q", sec)
				}
				if strings.Contains(old, sec) {
					t.Fatalf("redactAtMain kept %q in %q", sec, old)
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
		old, got := redactAtMain(in), Redact(in)
		if strings.Contains(got, secret) || old != got {
			t.Fatalf("%q\n old %q\n new %q", in, old, got)
		}
	}
	for _, in := range []string{
		"\u017Fecret=" + secret,
		" \u017Fecret=" + secret,
	} {
		old, got := redactAtMain(in), Redact(in)
		if !strings.Contains(old, secret) || strings.Contains(got, secret) {
			t.Fatalf("want main to keep and Redact to drop %q\n old %q\n new %q", in, old, got)
		}
	}
}

func TestTokenTailStillRedacts(t *testing.T) {
	gh := "ghp_" + strings.Repeat("a", 20)
	in := gh + "_tail"
	old, got := redactAtMain(in), Redact(in)
	if strings.Contains(old, gh) {
		t.Fatalf("main kept %s in %q", gh, old)
	}
	if strings.Contains(got, gh) {
		t.Fatalf("Redact kept %s\n old %q\n new %q", gh, old, got)
	}
}
