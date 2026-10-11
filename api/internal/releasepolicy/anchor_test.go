package releasepolicy

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// errSiblingAbsent is the skip reason when this checkout's sibling
// bifrost-trade-infra is not on disk. Passing the form check without that
// file would accept any well-formed fingerprint.
var errSiblingAbsent = errors.New("sibling bifrost-trade-infra checkout is absent, so OwnerKeyFingerprint was not compared with allowed_signers")

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

// TestOwnerKeyFingerprintMatchesAllowedSigners compares the compiled
// constant with the one key in the sibling infra checkout. A well-formed
// fingerprint of a different key does not pass. When that checkout is not
// beside this one, the test skips with errSiblingAbsent instead of passing.
func TestOwnerKeyFingerprintMatchesAllowedSigners(t *testing.T) {
	path, err := allowedSignersPath("")
	if err != nil {
		if errors.Is(err, errSiblingAbsent) {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	assertFingerprintIsTheAllowedSigner(t, path)
}

func TestOwnerKeyFingerprintSkipReasonWhenSiblingIsAbsent(t *testing.T) {
	_, err := allowedSignersPath(t.TempDir())
	if !errors.Is(err, errSiblingAbsent) {
		t.Fatalf("absent checkout = %v", err)
	}
	if err.Error() != errSiblingAbsent.Error() {
		t.Fatalf("skip reason = %q", err)
	}
}

func assertFingerprintIsTheAllowedSigner(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	keys := SignerKeys(string(raw))
	if len(keys) != 1 {
		t.Fatalf("allowed_signers at %s lists %d keys, want exactly one", path, len(keys))
	}
	if got := ssh.FingerprintSHA256(keys[0]); got != OwnerKeyFingerprint {
		t.Fatalf("allowed_signers fingerprint %s, OwnerKeyFingerprint %s", got, OwnerKeyFingerprint)
	}
}

// allowedSignersPath finds agent-config/release-policy/allowed_signers.
// A non-empty root is used only for that directory (the absent-checkout
// case). An empty root walks up from the working directory and this file.
func allowedSignersPath(root string) (string, error) {
	if root != "" {
		for _, cand := range []string{
			filepath.Join(root, "bifrost-trade-infra", "agent-config", "release-policy", "allowed_signers"),
			filepath.Join(root, "agent-config", "release-policy", "allowed_signers"),
		} {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, nil
			}
		}
		return "", errSiblingAbsent
	}
	var starts []string
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(file))
	}
	seen := map[string]bool{}
	for _, start := range starts {
		dir := start
		for i := 0; i < 8; i++ {
			if seen[dir] {
				break
			}
			seen[dir] = true
			cand := filepath.Join(dir, "bifrost-trade-infra", "agent-config", "release-policy", "allowed_signers")
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				return cand, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", errSiblingAbsent
}
