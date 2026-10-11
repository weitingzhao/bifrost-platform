package releasepolicy

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The compiled-in trust anchor is either empty (no key yet) or a well-formed
// OpenSSH SHA256 fingerprint: a typo here would make every signature fail
// while the status still claims an anchor exists.
func TestOwnerKeyFingerprintIsWellFormed(t *testing.T) {
	if OwnerKeyFingerprint == "" {
		return
	}
	rest, ok := strings.CutPrefix(OwnerKeyFingerprint, "SHA256:")
	if !ok {
		t.Fatalf("OwnerKeyFingerprint %q does not start with SHA256:", OwnerKeyFingerprint)
	}
	sum, err := base64.RawStdEncoding.DecodeString(rest)
	if err != nil {
		t.Fatalf("OwnerKeyFingerprint %q is not unpadded base64: %v", OwnerKeyFingerprint, err)
	}
	if len(sum) != 32 {
		t.Fatalf("OwnerKeyFingerprint decodes to %d bytes, want 32", len(sum))
	}
}
