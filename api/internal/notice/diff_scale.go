//go:build !race

package notice

import "os"

// diffRandomN is the seeded half of TestRedactHidesEverySecretMainHides.
const diffRandomN = 200_000

// CleanCorpusN is how many seeded clean strings the door tests place in every
// agent-written field. CI runs `go test ./... -timeout=120s` on two CPUs, and
// 100_000 composes per notification kind did not finish there (2026-10-11,
// ci-platform-c2fcj: approvalnotify timed out at 120 s; the same run takes 54 s
// on the development machine). The default fits CI;
// BIFROST_NOTICE_FULL_CORPUS=1 runs the full 100_000.
func CleanCorpusN() int {
	if os.Getenv("BIFROST_NOTICE_FULL_CORPUS") == "1" {
		return 100_000
	}
	return 10_000
}
