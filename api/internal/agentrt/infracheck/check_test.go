package infracheck

import (
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
)

// The original was captured from store/repos.go at 12a10541, then verified
// unchanged in migration commit 925d68b9. S6 subsequently adds only the missing
// entry guard and scripts/landing; check.golden was patched from that snapshot.
// Keep the original as evidence, with no update-golden switch.
func TestInfraCheckGolden(t *testing.T) {
	baseline, err := os.ReadFile("testdata/check-before-s6.golden")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprintf("%x", sha256.Sum256(baseline)) != "609576b234d3769588d5cfc6e22fca97da965f27a0791002f2b9809349a1e57e" {
		t.Fatal("pre-migration golden changed")
	}
	want, err := os.ReadFile("testdata/check.golden")
	if err != nil {
		t.Fatal(err)
	}
	if Check() != string(want) {
		t.Fatal("infra quick script differs byte-for-byte from the S6 golden")
	}
}
