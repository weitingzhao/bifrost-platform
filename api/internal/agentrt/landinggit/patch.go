package landinggit

import (
	"crypto/sha256"
	"encoding/hex"
)

// CandidatePatch is the Git half of a landing grant's material (coordinator
// v3 §5.6 item 3): the full patch M0..R and its sha256, the patch shown to
// the Owner (cut at ShownLimit bytes), and whether R's patch is A's (stable
// patch-id of M0..R against A's own parent..A; for a batch's R, of M0 to the
// stack's first commit, the lead's own). It is read with the
// environment's Git identity in a temporary clone that is removed after.
type CandidatePatch struct {
	SHA256    string
	Shown     string
	Truncated bool
	Size      int
	Match     bool
}

// ShownLimit is the most of a patch the material page shows (§5.6 item 3).
const ShownLimit = 512 << 10

// patchEvidence is the digest of the whole patch and what of it is shown:
// all of it up to ShownLimit bytes, none past it (the digest still covers
// every byte; the Owner then reads the file table and Gitea's compare).
func patchEvidence(patch string) CandidatePatch {
	sum := sha256.Sum256([]byte(patch))
	out := CandidatePatch{SHA256: hex.EncodeToString(sum[:]), Size: len(patch), Shown: patch, Truncated: len(patch) > ShownLimit}
	if out.Truncated {
		out.Shown = ""
	}
	return out
}
