package s5check

import (
	_ "embed"
	"encoding/json"
	"sync"
)

// The reviewed policy manifest compiled into this binary. The platform
// server compares the deployed checker's pinned profileDigest with it, so a
// checker image whose profile differs from the reviewed source cannot be
// adopted (S5 stays closed until both are updated by the core lane).
//
//go:embed policies/manifest.json
var embeddedManifest []byte

var embeddedDigest struct {
	once   sync.Once
	digest string
	err    error
}

// EmbeddedPolicyDigest is Digest of the compiled-in manifest — the same value
// CheckS5Candidate binds as the key's policy (profile) digest.
func EmbeddedPolicyDigest() (string, error) {
	embeddedDigest.once.Do(func() {
		embeddedDigest.digest, embeddedDigest.err = Digest(json.RawMessage(embeddedManifest))
	})
	return embeddedDigest.digest, embeddedDigest.err
}
