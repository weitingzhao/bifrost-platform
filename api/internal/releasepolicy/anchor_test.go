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
	b64, ok := strings.CutPrefix(OwnerKeyFingerprint, "SHA256:")
	if !ok {
		t.Fatalf("anchor %q does not start with SHA256:", OwnerKeyFingerprint)
	}
	raw, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil || len(raw) != 32 {
		t.Fatalf("anchor %q is not 32 bytes of unpadded base64 (%d, %v)", OwnerKeyFingerprint, len(raw), err)
	}
}
