package releasepolicy

// OwnerKeyFingerprint is `ssh-keygen -lf ~/.ssh/bifrost_release_owner.pub`
// (the SHA256:… field) of the Owner's signing key, ED25519, generated and
// read out by the Owner on 2026-10-15 (O18). Release policies and agent
// runtime rules verify against it; no allowed_signers file is kept, the
// fingerprint is the anchor. Changing it is a trust-anchor change: release.sh
// and this package refuse to auto-approve a diff that touches it.
//
// Empty means the Owner has no key: no policy or signed rule is valid then
// and every tier C release waits for a manual approval.
const OwnerKeyFingerprint = "SHA256:pzjcenBSbCbgIoqamoQLsvlxELfp4ikcLbvp164Eel8"
