package agentd

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
)

// patchLimit: a checkpoint's patch over 1 MiB is recorded as too large, not
// kept (design v5 §5.2 检查点).
const patchLimit = 1 << 20

// checkpoint samples a running attempt (design v5 §5.2 检查点): the log's
// tail and the workspace's patch against the base, which is its net change,
// the executor's own commits included. A sample's request (its number, the
// artifacts it names, whether the patch was too large) is written to the
// registry before it is sent and resent as it is, until the platform
// confirms it; only then is the next sample taken (review 164 A6). A sample
// that cannot be taken now is left for the next tick.
//
// whole says the sample this call took is all the attempt left that a sample
// takes, and the platform has it (TD-415, review 499 A-1): the log read (or
// none), its tail and the patch taken, the patch not too large, the attempt
// one with a repo, and the platform's confirmation in. A pending sample
// resent, one with a part refused, unread or too large, is not: the
// workspace a takeover ends is then kept.
func (d *Daemon) checkpoint(ctx context.Context, a *Attempt, ws *Workspace, entry *Entry) (whole bool) {
	if entry.Pending == nil {
		p := &PendingCheckpoint{Seq: entry.CheckpointSeq + 1}
		tail, err := readTail(filepath.Join(a.Out, "log.txt"), logTailBytes)
		whole = ws != nil && (err == nil || errors.Is(err, fs.ErrNotExist))
		if len(tail) > 0 {
			id, err := d.Client.PutArtifact(ctx, "log_tail", "text/plain; charset=utf-8", a.handText("log_tail", tail))
			switch {
			case err == nil:
				p.Artifacts = append(p.Artifacts, id)
			case transient(err):
				d.logf("agentd: checkpoint %s: log tail: %v", entry.AttemptID, err)
				return false
			default:
				whole = false
			}
		}
		if ws != nil {
			patch, tooLarge, err := ws.Diff(ctx, patchLimit)
			switch {
			case err != nil:
				d.logf("agentd: checkpoint %s: patch: %v", entry.AttemptID, err)
				return false
			case tooLarge:
				p.TooLarge, whole = true, false
			case len(patch) > 0:
				id, err := d.Client.PutArtifact(ctx, "checkpoint_patch", "text/x-diff", patch)
				switch {
				case err == nil:
					p.Artifacts = append(p.Artifacts, id)
				case transient(err):
					d.logf("agentd: checkpoint %s: patch: %v", entry.AttemptID, err)
					return false
				default:
					whole = false
				}
			}
		}
		entry.Pending, entry.CheckpointSeq = p, p.Seq
	}
	// The request is on disk before it is sent, every time: a pending one
	// whose first write failed is written again first (review 165 A1).
	if err := d.Registry.Put(*entry); err != nil {
		d.logf("agentd: checkpoint %s: %v", entry.AttemptID, err)
		return false
	}
	p := entry.Pending
	if err := d.Client.Checkpoint(ctx, entry.AttemptID, entry.Epoch, p.Seq, p.Artifacts, p.TooLarge); err != nil {
		d.logf("agentd: checkpoint %s #%d: %v", entry.AttemptID, p.Seq, err)
		return false
	}
	entry.Confirmed, entry.Pending = p.Seq, nil
	if err := d.Registry.Put(*entry); err != nil {
		d.logf("agentd: checkpoint %s: %v", entry.AttemptID, err)
	}
	return whole
}
