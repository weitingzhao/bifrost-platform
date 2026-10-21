package agentd

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

// What an attempt that failed before its executor started hands in
// (TD-394). A failed setup (setup.go) used to end the attempt with exit
// code 3 and nothing else, and a checkout or a sibling that could not be had
// (failBefore) with nothing at all: why was only in agentd's journal on the
// agent host, which only the Owner reads (PROD M-22: nine attempts, the
// cause unknown). Now agentd writes a report of what failed (the setup's
// failed command, its exit code and the end of its output; or the error that
// ended the preparation), masked of local paths as any report is (handText;
// the setup runs with the allowed variables only, no credential among them),
// and hands it in with the log's tail as one last
// checkpoint, the route a running attempt already uses: the attempt names
// them in its attempt_checkpointed event. The attempt still ends as it did.

// prepareReportName is the report's title: the line a reader of the
// attempt's artifacts finds it by.
const prepareReportName = "# The attempt failed before its executor started"

// prepareRecords are the records of what runs before the executor, each
// written once by its stage, a failed one's with pass false: the setup's
// (RunSetup) and the test database's (RunTestDB, TD-403), with what the
// report says failed.
var prepareRecords = []struct{ file, what string }{
	{"setup.json", "The task's setup failed"},
	{"testdb.json", "The attempt's test database did not start"},
}

// setupFailure is the record of what failed before the attempt's executor,
// and what it was, read as any file the attempt's output holds, never
// through a link.
func setupFailure(out string) (Verification, bool) {
	v, _, failed := prepareFailure(out)
	return v, failed
}

func prepareFailure(out string) (Verification, string, bool) {
	for _, r := range prepareRecords {
		var v Verification
		b, err := readAtMost(filepath.Join(out, r.file), maxBody)
		if err == nil && json.Unmarshal(b, &v) == nil && !v.Pass {
			return v, r.what, true
		}
	}
	return Verification{}, "", false
}

// isSetupFailure says whether an executor's end is its setup's failure: the
// launcher exited with setupFailed, and the setup's record says it failed.
func isSetupFailure(endHow string, code int, out string) bool {
	if endHow != "exit" || code != setupFailed {
		return false
	}
	_, failed := setupFailure(out)
	return failed
}

// setupReportOf is the report of what failed before the executor, whose
// record is in out.
func setupReportOf(out string) []byte {
	v, what, _ := prepareFailure(out)
	b, _ := json.Marshal(v)
	return []byte(prepareReportName + "\n\n" + what + ", so the executor did not start. " + failedCommand(b, tailBytes))
}

// beforeReport is the report of an attempt that ended before its executor
// was launched: why, as agentd's error says.
func beforeReport(err error) []byte {
	return []byte(prepareReportName + "\n\nagentd could not prepare the attempt, so the executor did not start:\n\n" + fenced(err.Error()))
}

// reportPrepare hands in body, the report of a failure before the executor,
// and the log's tail as a checkpoint of their own, once: a sample still
// pending is sent first, as it is; the report's request is journalled before
// it is sent and resent as it is (as a sample's is, review 164 A6). An upload
// or a checkpoint the platform refuses is logged and not tried again; a
// transient failure is returned, so the hand-over is taken up again; so is a
// fence.
func (d *Daemon) reportPrepare(ctx context.Context, a *Attempt, e *Entry, body []byte) error {
	if e.PrepareReported {
		return nil
	}
	if e.PrepareSeq == 0 {
		if err := d.sendPending(ctx, e); err != nil {
			return err
		}
		var ids []string
		id, err := d.putRetried(ctx, "report", "text/markdown; charset=utf-8", a.handText("report", body))
		switch {
		case err == nil:
			ids = append(ids, id)
		case transient(err):
			return err
		default:
			d.logf("agentd: the report of %s's preparation refused: %v", e.AttemptID, err)
		}
		if tail, _ := readTail(filepath.Join(a.Out, "log.txt"), logTailBytes); len(tail) > 0 {
			id, err = d.putRetried(ctx, "log_tail", "text/plain; charset=utf-8", a.handText("log_tail", tail))
			switch {
			case err == nil:
				ids = append(ids, id)
			case transient(err):
				return err
			default:
				d.logf("agentd: the log tail of %s refused: %v", e.AttemptID, err)
			}
		}
		if len(ids) == 0 {
			e.PrepareReported = true
			return d.Registry.Put(*e)
		}
		e.CheckpointSeq++
		e.PrepareSeq, e.Pending = e.CheckpointSeq, &PendingCheckpoint{Seq: e.CheckpointSeq, Artifacts: ids}
		if err = d.Registry.Put(*e); err != nil {
			return err
		}
	}
	if err := d.sendPending(ctx, e); err != nil {
		return err
	}
	e.PrepareReported = true
	d.logf("agentd: %s: handed in what failed before its executor started (checkpoint %d)", e.AttemptID, e.PrepareSeq)
	return d.Registry.Put(*e)
}

// sendPending sends the entry's pending checkpoint, if any, until the
// platform takes it or refuses it for good (logged, then dropped).
func (d *Daemon) sendPending(ctx context.Context, e *Entry) error {
	p := e.Pending
	if p == nil {
		return nil
	}
	err := d.retry(ctx, func() error { return d.Client.Checkpoint(ctx, e.AttemptID, e.Epoch, p.Seq, p.Artifacts, p.TooLarge) })
	switch {
	case err == nil:
		e.Confirmed = p.Seq
	case transient(err), errors.Is(err, ErrFenced):
		return err
	default:
		d.logf("agentd: checkpoint %d of %s refused: %v", p.Seq, e.AttemptID, err)
	}
	e.Pending = nil
	return d.Registry.Put(*e)
}
