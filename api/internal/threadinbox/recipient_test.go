package threadinbox

import (
	"fmt"
	"strings"
	"testing"
)

func TestRecipientSessionLengthAndCanonicalReplay(t *testing.T) {
	t.Parallel()
	for _, n := range []int{1, 36, 121, 122, 128, 129} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			t.Parallel()
			id := strings.Repeat("a", n)
			want := "thread:" + id
			if n > 128 {
				want = ""
			}
			for _, label := range []string{id, "thread:" + id, id + " (W-31)", "thread:" + id + " (W-31)"} {
				got := Recipient(label)
				if got != want {
					t.Errorf("Recipient(%q) = %q, want %q", label, got, want)
				}
				if again := Recipient(got); again != got {
					t.Errorf("canonical recipient rejected: %q -> %q", got, again)
				}
			}
		})
	}
}

func TestRecipientRequiresPrimaryIdentifier(t *testing.T) {
	t.Parallel()
	for _, label := range []string{
		"thread:",
		"thread:thread:local_requester",
		"thread:thread:local_requester (W-31)",
		"https://example.test/local_victim",
		"https://example.test/?next=local_victim",
		"https://example.test/#thread:local_victim",
		"/notes?next=thread:local_victim",
		"session-1 (see https://example.test/?next=thread:local_victim)",
		"session-1 (do not notify thread:local_victim)",
		"local_requester (thread:local_victim)",
		"notes (thread:local_victim)",
		"实现线程（thread:local_victim）",
		"local_requester and thread:local_requester",
	} {
		if got := Recipient(label); got != "" {
			t.Errorf("Recipient(%q) = %q, want rejection", label, got)
		}
	}
	for label, want := range map[string]string{
		"session-1":                     "thread:session-1",
		"local_requester (TD-457)":      "thread:local_requester",
		"session-1 (implementation)":    "thread:session-1",
		"thread:local_requester (W-31)": "thread:local_requester",
		"thread:agent:review":           "thread:agent:review",
		"thread:owner":                  "thread:owner",
		" local_requester ":             "thread:local_requester",
	} {
		if got := Recipient(label); got != want {
			t.Errorf("Recipient(%q) = %q, want %q", label, got, want)
		}
		if got := Recipient(Recipient(label)); got != want {
			t.Errorf("Recipient rejected canonical form of %q: %q", label, got)
		}
	}
}
