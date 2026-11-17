//bifrost:owner coordination
package server

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// 2026-11-17, the main-head table in nextgen/run-readers.md: six display
// readers each asked Gitea where main is. They now ask mainOf, which reads
// the ledger for repositories gate-git watches. These cases pin when it may
// and may not answer from the ledger.
func TestLedgeredMain(t *testing.T) {
	ledgerSHA := strings.Repeat("a", 40)
	giteaSHA := strings.Repeat("b", 40)
	gitea := func(context.Context, string) (string, error) { return giteaSHA, nil }
	ledger := func(answer string, err error) mainReader {
		return func(context.Context, string) (string, error) { return answer, err }
	}
	watched := []string{"bifrost-platform"}
	cases := []struct {
		name string
		last mainReader
		repo string
		want string
	}{
		{"watched repository: the ledger", ledger(ledgerSHA, nil), "bifrost-platform", ledgerSHA},
		{"not watched: Gitea, the ledger has no hook for it", ledger(ledgerSHA, nil), "bifrost-trade-infra", giteaSHA},
		{"ledger said nothing yet: Gitea", ledger("", nil), "bifrost-platform", giteaSHA},
		{"ledger unreadable: Gitea", ledger("", errors.New("down")), "bifrost-platform", giteaSHA},
		{"ledger answer is not a commit: Gitea", ledger("main", nil), "bifrost-platform", giteaSHA},
		{"no ledger on this platform: Gitea", nil, "bifrost-platform", giteaSHA},
	}
	for _, c := range cases {
		got, err := ledgeredMain(watched, c.last, gitea)(context.Background(), c.repo)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q, %v; want %q", c.name, got, err, c.want)
		}
	}
}
