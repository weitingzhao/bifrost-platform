// Package mustrefuse holds the execution port's must-refuse set: requests the
// runner must refuse however its code is changed (Owner 2026-11-15; CI line:
// the runner's package is a trust anchor, and its must-refuse directory is in
// the signed policy's owner_only group). Definitions and tests live in this
// directory only. The tests use the runner through its exported surface, with
// a fake platform-api, a made-up seed and no secret of any kind.
package mustrefuse
