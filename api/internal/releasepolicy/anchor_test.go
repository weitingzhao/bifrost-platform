package releasepolicy

import (
	"encoding/base64"
	"strings"
	"testing"
)

// The anchor is a whole SHA-256 key fingerprint as ssh-keygen prints it: a
// copy that lost or changed a character would refuse every signature, or
// trust the wrong key.
func TestOwnerKeyFingerprintIsAWholeSHA256(t *testing.T) {
	wholeSHA256(t, OwnerKeyFingerprint)
}

func wholeSHA256(t *testing.T, fingerprint string) {
	t.Helper()
	b64, ok := strings.CutPrefix(fingerprint, "SHA256:")
	if !ok {
		t.Fatalf("anchor %q does not start with SHA256:", fingerprint)
	}
	raw, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil || len(raw) != 32 {
		t.Fatalf("anchor %q is not 32 bytes of unpadded base64 (%d, %v)", fingerprint, len(raw), err)
	}
}

// Origin: 2026-11-15, TD-576. The second signing key's fingerprint is copied
// by hand from a card's output into the source: empty (no such key) or whole,
// and never the first key again, which would read as two keys and be one.
func TestClusterSignerFingerprintIsEmptyOrAWholeSHA256(t *testing.T) {
	if ClusterSignerFingerprint == "" {
		if PolicyAnchors() != OwnerKeyFingerprint {
			t.Fatalf("with no cluster signer the policy anchors are %q, want the Owner key alone", PolicyAnchors())
		}
		return
	}
	wholeSHA256(t, ClusterSignerFingerprint)
	if ClusterSignerFingerprint == OwnerKeyFingerprint {
		t.Fatal("the cluster signer is the Owner key")
	}
	if PolicyAnchors() != OwnerKeyFingerprint+" "+ClusterSignerFingerprint {
		t.Fatalf("policy anchors = %q", PolicyAnchors())
	}
}

func TestJoinAnchorsLeavesOutEmptyOnes(t *testing.T) {
	if got := joinAnchors("", "SHA256:a", "", "SHA256:b"); got != "SHA256:a SHA256:b" {
		t.Fatalf("got %q", got)
	}
	if got := joinAnchors("", ""); got != "" {
		t.Fatalf("got %q, want empty: no key means nothing verifies", got)
	}
}
