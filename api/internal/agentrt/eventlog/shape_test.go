package eventlog

import "testing"

// readShapeLock is the fingerprint recorded for each version of the read
// shape. A change of ReadShape without a new version fails here: raise
// ReadShapeVersion, record the new fingerprint under it, and tell the
// packages that read the ledger (codeguard's ledgerSQL lists them).
//
// Origin: 2026-11-16, the read shape became the contract of the ledger's
// reads; the bus registry locks its channels' contracts the same way
// (bus/registry/contract.lock.json).
var readShapeLock = map[int]string{
	1: "sha256:d46d1ce8c68dea99f490bb97aa980c4534099286af0dc7a829b5b82fa2d5d3d5",
}

func TestReadShapeChangesOnlyWithItsVersion(t *testing.T) {
	want, ok := readShapeLock[ReadShapeVersion]
	if !ok {
		t.Fatalf("no fingerprint is recorded for read shape version %d", ReadShapeVersion)
	}
	if got := ReadShapeFingerprint(); got != want {
		t.Fatalf("the read shape changed without its version: fingerprint %s, version %d records %s — raise ReadShapeVersion and record the new fingerprint", got, ReadShapeVersion, want)
	}
	// The positive control: the fingerprint moves when a meaning does.
	was := ReadShape[0].Meaning
	ReadShape[0].Meaning = was + " (changed)"
	moved := ReadShapeFingerprint() != want
	ReadShape[0].Meaning = was
	if !moved {
		t.Fatal("the fingerprint does not see a changed meaning")
	}
}
