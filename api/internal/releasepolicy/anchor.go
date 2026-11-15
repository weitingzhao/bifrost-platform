package releasepolicy

// OwnerKeyFingerprint is `ssh-keygen -lf ~/.ssh/bifrost_release_owner.pub`
// (the SHA256:… field) of the Owner's signing key, ED25519, generated and
// read out by the Owner on 2026-10-15 (O18; the first key of that day was
// replaced the same day, its passphrase lost before any signature). Release
// policies and agent runtime rules verify against it; no allowed_signers file
// is kept, the fingerprint is the anchor. Changing it is a trust-anchor
// change: release.sh and this package refuse to auto-approve a diff that
// touches it.
//
// Empty means the Owner has no key: no policy or signed rule is valid then
// and every tier C release waits for a manual approval.
const OwnerKeyFingerprint = "SHA256:HQBmsFUm1mh0JHKwiG/AOJesGSRNNZF+waMq4BI1M7k"

// ClusterSignerFingerprint is the fingerprint of the second key that may sign
// a release policy and lift a freeze: an ED25519 key generated inside the
// cluster by an owner_run_command card the Owner approved with his one-time
// code, kept in Secret bifrost-owner-exec/release-policy-signer and used only
// by commands of such cards (Owner 2026-11-15, TD-576: re-signing must not
// need the computer that holds the first key; a code-approved card already
// proves that he agreed to those exact bytes, and the identity it runs as is
// cluster administrator). The private key is never printed and no agent
// identity reads that namespace's Secrets.
//
// The value is the line card #572 printed on 2026-11-15 (its command ran
// ssh-keygen -lf on the public half; output_sha256 2723e3d2…).
//
// Empty means there is no such key and only OwnerKeyFingerprint signs. To
// withdraw it, set it to empty and release: a policy it signed stops
// verifying, and the Owner signs again with the first key.
//
// The agent runtime's rules do not accept it: they verify against
// OwnerKeyFingerprint alone.
const ClusterSignerFingerprint = "SHA256:KWzT2m89T/mfDAFBmzOJvyDSu/lPwzhx8alEntAXz9Q"

// PolicyAnchors is what a release policy and a freeze lift verify against:
// the fingerprints that may sign them, separated by a space.
func PolicyAnchors() string {
	return joinAnchors(OwnerKeyFingerprint, ClusterSignerFingerprint)
}

func joinAnchors(fingerprints ...string) string {
	out := ""
	for _, fp := range fingerprints {
		if fp == "" {
			continue
		}
		if out != "" {
			out += " "
		}
		out += fp
	}
	return out
}
