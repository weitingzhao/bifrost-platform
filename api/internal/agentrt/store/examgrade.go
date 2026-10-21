package store

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// Answers and grading (exam.go). A part of a run is taken only for a paper
// issued to the host that hands it in, not graded yet, not lapsed, of the
// same set and executor; each answer at most once a run of an item (the same
// one again is taken as it was; another is refused). The answer is counts:
// what was taken, how many of the paper's runs have one, and, once graded,
// the record's event and rows; never a rule's verdict (the cut §5).

// RecordExamAnswers takes a part of a run's answers that host hands in and
// grades the run once every run of its paper has an answer.
func RecordExamAnswers(ctx context.Context, db DB, host, token string, sub ExamSubmission, set ExamSet) (map[string]any, error) {
	return recordExamAnswers(ctx, db, host, token, sub, set, 0)
}

func recordExamAnswers(ctx context.Context, db DB, host, token string, sub ExamSubmission, set ExamSet, later time.Duration) (map[string]any, error) {
	if err := checkSubmission(sub); err != nil {
		return nil, err
	}
	if host == "" || token == "" {
		return nil, invalid("answers are handed in by a host's token")
	}
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		now, err := examClock(ctx, tx, later)
		if err != nil {
			return err
		}
		p, err := findPaper(ctx, tx, sub.RunID)
		if err != nil {
			return err
		}
		switch {
		case p.Host != host:
			return fmt.Errorf("%w: run %s", ErrOtherHost, sub.RunID)
		case p.Executor != sub.Executor:
			return invalid("run %s was issued to %s, not %s", sub.RunID, p.Executor, sub.Executor)
		case p.SetSHA256 != sub.ExamSHA256:
			return invalid("exam_sha256 is not the set run %s was issued from", sub.RunID)
		}
		onPaper := map[string]ExamPaperItem{}
		expected := 0
		for _, it := range p.Items {
			onPaper[it.Item] = it
			expected += len(it.Runs)
		}
		had, err := takenAnswers(ctx, tx, p.event)
		if err != nil {
			return err
		}
		// Each answer as it would be kept: the same as one taken is a repeat;
		// another for a run that has one is refused.
		var fresh []examAnswer
		repeated := 0
		for _, a := range sub.Answers {
			it, ok := onPaper[a.Item]
			switch {
			case !ok || !slices.Contains(it.Runs, a.RunNo):
				return invalid("run %s has no item %s run %d", sub.RunID, a.Item, a.RunNo)
			case a.Round > it.Rounds:
				return invalid("item %s takes at most %d rounds, not %d", a.Item, it.Rounds, a.Round)
			}
			doc := examAnswer{RunID: sub.RunID, Item: a.Item, RunNo: a.RunNo, Round: a.Round, Executor: sub.Executor, Provider: sub.Provider,
				Model: sub.Model, ToolVersion: sub.ToolVersion, WallS: a.WallS, Conclusion: a.Conclusion, OutOfWork: a.OutOfWorkspace, Text: a.Text}
			if prev, ok := had[runKey{a.Item, a.RunNo}]; ok {
				if prev.examAnswer != doc {
					return fmt.Errorf("%w: run %s item %s run %d was answered before with another answer", ErrConflict, sub.RunID, a.Item, a.RunNo)
				}
				repeated++
				continue
			}
			fresh = append(fresh, doc)
		}
		voided, err := voidOf(ctx, tx, p.event)
		if err != nil {
			return err
		}
		if voided != nil {
			return fmt.Errorf("%w: run %s was voided by %s; it takes no answer", ErrState, sub.RunID, voided["approval"])
		}
		graded, err := gradedRun(ctx, tx, p.event)
		if err != nil {
			return err
		}
		if graded != nil {
			if len(fresh) > 0 {
				return fmt.Errorf("%w: run %s was graded; it takes no more answers", ErrState, sub.RunID)
			}
			out = map[string]any{"run_id": sub.RunID, "graded": true, "duplicate": true, "event_id": graded["event_id"], "rows": graded["rows"],
				"taken": 0, "repeated": repeated, "answered": len(had), "expected": expected}
			return nil
		}
		switch {
		case !now.Before(p.ExpiresAt):
			return fmt.Errorf("%w: run %s lapsed at %s", ErrState, sub.RunID, p.ExpiresAt.UTC().Format(time.RFC3339))
		case set.Digest() != p.SetSHA256:
			return fmt.Errorf("%w: the platform's exam set is not the one run %s was issued from; the run cannot be graded", ErrState, sub.RunID)
		}
		// One run, one executor: a part that says another provider, model or
		// tool version than the parts before it is another run.
		for _, t := range had {
			if t.Provider != sub.Provider || t.Model != sub.Model || t.ToolVersion != sub.ToolVersion {
				return invalid("run %s was answered by %s %s %s before, not %s %s %s", sub.RunID, t.Provider, t.Model, t.ToolVersion,
					sub.Provider, sub.Model, sub.ToolVersion)
			}
		}
		for _, doc := range fresh {
			body, merr := json.Marshal(doc)
			if merr != nil {
				return merr
			}
			if len(body) > maxArtifactBytes {
				return invalid("answer %s run %d is over 1 MiB as kept", doc.Item, doc.RunNo)
			}
			// An exam artifact is scanned as an uploaded one is (H8, T11):
			// agentd masks its paths before it hands an answer in.
			if lp, found := LocalPath("exam", body); found {
				return invalid("answer %s run %d holds a local path: %s", doc.Item, doc.RunNo, lp)
			}
			if ref, found := WorkspaceRef("exam", body); found {
				return invalid("answer %s run %d refers to a task's workspace: %s", doc.Item, doc.RunNo, ref)
			}
			id, perr := PutArtifact(ctx, tx, hostActor(host), "exam", "application/json", body)
			if perr != nil {
				return perr
			}
			if _, err = Append(ctx, tx, Event{Stream: ExamsStream, Type: "artifact_put", Actor: hostActor(host), Cause: p.event,
				Body: map[string]any{"artifact_id": id, "artifact_kind": "exam", "size_bytes": len(body), "uploaded_by": token}}); err != nil {
				return err
			}
			had[runKey{doc.Item, doc.RunNo}] = taken{artifact: id, examAnswer: doc}
		}
		out = map[string]any{"run_id": sub.RunID, "graded": false, "duplicate": false, "taken": len(fresh), "repeated": repeated,
			"answered": len(had), "expected": expected}
		if len(had) < expected {
			return nil
		}
		ev, rows, err := gradePaper(ctx, tx, set, p, had, now)
		if err != nil {
			return err
		}
		out["graded"], out["event_id"], out["rows"] = true, ev.EventID, rows
		return nil
	})
	return out, err
}

// gradeLapsed grades a paper that lapsed with answers, the runs without one
// not passed; one with none, or of a set the platform no longer has, records
// nothing.
func gradeLapsed(ctx context.Context, tx pgx.Tx, set ExamSet, p issued, now time.Time) error {
	if set.Digest() != p.SetSHA256 {
		return nil
	}
	had, err := takenAnswers(ctx, tx, p.event)
	if err != nil || len(had) == 0 {
		return err
	}
	_, _, err = gradePaper(ctx, tx, set, p, had, now)
	return err
}

// gradePaper grades every run of paper p with set, each item at the t0 it was
// rendered at: a run without an answer, whose seat read outside its
// workspace, or whose executor did not end done, is not passed. It records capability_scored on the exams stream,
// caused by the paper's event, and one capability_score row a question.
func gradePaper(ctx context.Context, tx pgx.Tx, set ExamSet, p issued, had map[runKey]taken, now time.Time) (Appended, int, error) {
	classOf := map[string]string{}
	for _, it := range set.Items(p.Job) {
		classOf[it.ID] = it.Class
	}
	type tally struct {
		attempts, passes int
		walls            []int
	}
	byClass := map[string]*tally{}
	results := []any{}
	var by examAnswer // who answered: every answer of a run says the same
	for _, it := range p.Items {
		class := classOf[it.Item]
		if !ExamClass(p.Job, class) {
			return Appended{}, 0, fmt.Errorf("store: paper %s item %s has no class in the set", p.RunID, it.Item)
		}
		c := byClass[class]
		if c == nil {
			c = &tally{}
			byClass[class] = c
		}
		for _, n := range it.Runs {
			c.attempts++
			r := map[string]any{"item": it.Item, "run_no": n}
			a, ok := had[runKey{it.Item, n}]
			switch {
			case !ok:
				r["verdict"] = "missing"
			case a.OutOfWork:
				r["verdict"] = "out_of_workspace"
			case a.Conclusion != "done":
				// The executor timed out, failed or left no report: at work the
				// controller takes nothing of such an attempt, so its text is
				// not graded (review 415 A1: a rule that asks only for no
				// alert would pass an empty answer).
				r["verdict"], r["conclusion"] = "not_done", a.Conclusion
			default:
				v, err := set.Grade(it.Item, a.Text, it.T0)
				if err != nil {
					return Appended{}, 0, fmt.Errorf("store: grading %s run %d: %w", it.Item, n, err)
				}
				r["verdict"], r["failed"], r["flagged"] = "fail", nonNil(v.Failed), nonNil(v.Flagged)
				if v.Passed {
					r["verdict"] = "pass"
					c.passes++
				}
			}
			if ok {
				by = a.examAnswer
				r["answer"], r["round"], r["wall_s"] = a.artifact, a.Round, a.WallS
				c.walls = append(c.walls, a.WallS)
			}
			results = append(results, r)
		}
	}
	classes := make([]string, 0, len(byClass))
	for k := range byClass {
		classes = append(classes, k)
	}
	sort.Strings(classes)
	samples := []any{}
	for _, k := range classes {
		c := byClass[k]
		samples = append(samples, map[string]any{"executor": p.Executor, "provider": by.Provider, "model": by.Model, "tool_version": by.ToolVersion,
			"job": p.Job, "class": k, "core": false, "attempts": c.attempts, "first_pass": c.passes})
	}
	started := p.IssuedAt.UTC().Format(time.RFC3339Nano)
	digest, err := canon.Sum(map[string]any{"run_id": p.RunID, "executor": p.Executor, "provider": by.Provider, "model": by.Model,
		"tool_version": by.ToolVersion, "questions": p.SetSHA256, "started_at": started, "results": results})
	if err != nil {
		return Appended{}, 0, err
	}
	body := map[string]any{"source": "exam", "rows": len(classes), "samples": samples, "run_id": p.RunID, "host": p.Host,
		"executor": p.Executor, "provider": by.Provider, "questions": p.SetSHA256, "results": results, "started_at": started, "digest": digest}
	if by.Model != "" {
		body["model"] = by.Model
	}
	ev, err := Append(ctx, tx, Event{Stream: ExamsStream, Type: "capability_scored", Actor: "controller", Cause: p.event, Body: body})
	if err != nil {
		return Appended{}, 0, err
	}
	start := p.IssuedAt
	if !start.Before(now) {
		start = now.Add(-time.Second)
	}
	for _, k := range classes {
		c := byClass[k]
		if _, err = tx.Exec(ctx, `INSERT INTO agentrt.capability_score (capability_score_id, executor_actor_id, model_provider, model,
		  tool_version, job_id, task_class, core, source, window_start, window_end, attempts, first_pass, accepted, rework,
		  median_wall_s, score, computed_from_event_id)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,false,'exam',$8,$9,$10,$11,$11,0,$12,$13,$14) ON CONFLICT DO NOTHING`,
			"s_"+newULID(now), p.Executor, by.Provider, by.Model, by.ToolVersion, p.Job, k, start, now,
			c.attempts, c.passes, median(c.walls), Wilson(c.passes, c.attempts), ev.EventID); err != nil {
			return Appended{}, 0, err
		}
	}
	return ev, len(classes), nil
}
