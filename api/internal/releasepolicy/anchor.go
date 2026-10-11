package releasepolicy

// OwnerKeyFingerprint is `ssh-keygen -lf ~/.ssh/bifrost_release_owner.pub`
// (the SHA256:… field). It must equal the key in
// bifrost-trade-infra/agent-config/release-policy/allowed_signers. Changing
// it is a trust-anchor change: release.sh and this package refuse to
// auto-approve a diff that touches it.
//
// Empty means the Owner has not generated the key yet. No policy is valid then
// and every tier C release waits for a manual approval.
//
// Set on 2026-10-11 from the key the Owner generated that day; he read the
// same fingerprint off his own terminal before approving the release that
// carried it.
const OwnerKeyFingerprint = "SHA256:pzjcenBSbCbgIoqamoQLsvlxELfp4ikcLbvp164Eel8"
