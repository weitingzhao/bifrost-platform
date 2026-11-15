package landinggit

import (
	"errors"
	"fmt"
)

// ChangeIdentity is what one accepted result changes, for the delivered-by
// backfill (DESIGN-step-landed-superseded-2026-11-01 §6 item 2): a result A
// is one commit on its task's base (hand-ins are squashed), so A^..A is the
// task's change. PatchID is `git patch-id --verbatim` of that diff: --stable
// strips whitespace, so it would equate a YAML indentation change with none.
// DiffSHA256 is the sha256 of the diff's own bytes (--binary, so full blob
// ids and binary content are in it).
type ChangeIdentity struct {
	PatchID, DiffSHA256 string
}

// ErrNotComparable: the two results cannot be compared, and reading them
// again gives the same answer: a SHA that is not one, a commit that is not
// one commit on one parent, a diff over the output bound. ErrNotServed is
// one too, but only inferred. Anything else (the network, a timeout) is a
// plain error worth trying again.
var ErrNotComparable = errors.New("the two results cannot be compared")

// ErrNotServed: the fetch of the two commits failed while the remote still
// answered. Most likely the remote no longer serves one of them (gc took an
// unreferenced commit), but a full temporary directory, an upload-pack
// error under load or a transfer cut short look the same from here, so it
// is worth reading again after a long wait, not never.
var ErrNotServed = fmt.Errorf("%w: the fetch failed while the remote answered", ErrNotComparable)
