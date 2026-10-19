package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// The chief of staff's qualifying exam as the platform gives it (W-31 2.4 ④;
// the exam's cut, infra agent-config/work/multi-agent/CUT-chief-exam-2026-10-19.md
// §6 甲, D-E1 the Owner 2026-10-19). A host's agentd asks for a paper for one
// executor (IssueExam): the platform renders every item of the job's set
// with the production spec functions at the paper's time and keeps the paper.
// The host runs each item ExamRuns times as at work and hands each answer in
// as it comes (RecordExamAnswers); once every run of the paper has its
// answer, the platform grades them with the same set at the t0 each item was
// rendered at, never a time the host gives. The rules and key points never
// leave the platform: the host gets specs, hands in text and is answered with
// counts.
//
// Everything is on stream ExamsStream, in types and tables that exist (no
// migration):
//   - a paper is an exam artifact the controller puts, recorded as
//     artifact_put. The run's id is the paper's sha256, so a run names its
//     paper, and an exam artifact someone uploaded (recorded on the artifacts
//     stream, by its writer) is no paper. artifact_put is the record of the
//     issue: what was put, its kind and size, and the token that asked
//     (uploaded_by); no other type says "this artifact was handed out".
//   - an answer is an exam artifact the host puts, recorded as artifact_put
//     caused by the paper's event: a run's answers are the events its paper
//     caused.
//   - a graded run is capability_scored (source exam) caused by the paper's
//     event, and one capability_score row a question: the job, the question's
//     class, not core, source exam.
//
// Routing reads none of it (competence.go reads ScoresStream and
// BenchmarksStream): passing the exam is what the Owner signs a candidate
// from probation to active on (the cut §6, §7.3), not a routing sample.

// ExamsStream is the stream of the exams: papers issued, answers taken, runs
// graded.
const ExamsStream = "capability_exams"

// ExamPaperLife is how long after its issue a paper takes answers. A paper
// that lapses with answers is graded when a paper is next asked for, the runs
// without one counted as not passed (the cut §6: 缺的记不过); one with none
// records nothing.
const ExamPaperLife = 24 * time.Hour

// ExamRuns is how many times each item of a paper is run (k, the cut §3.1).
const ExamRuns = 3

// examLock serializes the exams' writes: one open paper an executor, one
// answer a run of an item, a paper graded once.
const examLock = 742113

// examClasses are the task classes each job's exam scores its questions
// under: one capability_score row a question (the cut §6).
var examClasses = map[string][]string{
	"chief_of_staff": {"exam-brief", "exam-card", "exam-alert", "exam-refuse", "exam-handover"},
}

// ExamClass says whether class is one job's exam scores a question under.
func ExamClass(job, class string) bool { return slices.Contains(examClasses[job], class) }

// ErrOtherHost: the run's paper was issued to another host; nothing was
// written.
var ErrOtherHost = errors.New("store: the paper was issued to another host")

// ExamSet is a question set as the platform gives and grades it. Package exam
// implements it (exam.Platform); the store cannot import exam, which renders
// through the store's own spec functions.
type ExamSet interface {
	// Digest is the set's sha256.
	Digest() string
	// Items are the items of job's exam, in the set's order.
	Items(job string) []ExamItem
	// Render is item rendered for a paper issued at now.
	Render(item string, now time.Time) (ExamSpec, error)
	// Grade grades answer to item rendered at t0.
	Grade(item, answer string, t0 time.Time) (ExamVerdict, error)
}

// ExamItem is one item of an exam and the class of its question.
type ExamItem struct{ ID, Class string }

// ExamSpec is an item rendered: the t0 its records were filled at, the spec
// the seat reads, and for a plan the repository it reads at a commit.
type ExamSpec struct {
	T0               time.Time
	Spec             []byte
	Repo, BaseCommit string
}

// ExamVerdict is one run graded: passed when every hard rule held; Failed
// are the hard rules that did not, Flagged the soft ones.
type ExamVerdict struct {
	Passed          bool
	Failed, Flagged []string
}

// ExamRequest is a host's request for a paper: the host, the token that
// asks, the job and the executor that will sit it.
type ExamRequest struct{ Host, Token, Job, Executor string }

// ExamPaper is a paper as it is kept (an exam artifact) and handed out, the
// run's id added: the paper's sha256.
type ExamPaper struct {
	RunID     string          `json:"run_id,omitempty"`
	Job       string          `json:"exam"`
	SetSHA256 string          `json:"set_sha256"`
	Host      string          `json:"host"`
	Executor  string          `json:"executor"`
	Nonce     string          `json:"nonce"`
	IssuedAt  time.Time       `json:"issued_at"`
	T0        time.Time       `json:"t0"`
	ExpiresAt time.Time       `json:"expires_at"`
	Items     []ExamPaperItem `json:"items"`
}

// ExamPaperItem is one item of a paper: its spec, the t0 it was rendered at,
// the runs it is run for and the rounds each run may take (a plan the
// controller refuses goes back once more, as at work: max_rounds 2).
type ExamPaperItem struct {
	Item       string          `json:"item"`
	T0         time.Time       `json:"t0"`
	Runs       []int           `json:"runs"`
	Rounds     int             `json:"rounds"`
	Repo       string          `json:"repo,omitempty"`
	BaseCommit string          `json:"base_commit,omitempty"`
	Spec       json.RawMessage `json:"spec"`
}

// ExamSubmission is a part of a run's answers as agentd hands it in.
type ExamSubmission struct {
	RunID       string       `json:"run_id"`
	Executor    string       `json:"executor"`
	Provider    string       `json:"provider"`
	Model       string       `json:"model"`
	ToolVersion string       `json:"tool_version"`
	ExamSHA256  string       `json:"exam_sha256"`
	PinsOK      bool         `json:"pins_ok"`
	Answers     []ExamAnswer `json:"answers"`
}

// ExamAnswer is one run of one item: the seat's last message, how long it
// took, how the executor ended, and whether the seat read outside its
// workspace (such a run is not passed).
type ExamAnswer struct {
	Item           string `json:"item"`
	RunNo          int    `json:"run_no"`
	Round          int    `json:"round"`
	Text           string `json:"text"`
	WallS          int    `json:"wall_s"`
	Conclusion     string `json:"conclusion"`
	OutOfWorkspace bool   `json:"out_of_workspace"`
}

// examAnswer is an answer as it is kept: the run it belongs to and who gave
// it, so the same text for another run is another artifact.
type examAnswer struct {
	RunID       string `json:"run_id"`
	Item        string `json:"item"`
	RunNo       int    `json:"run_no"`
	Round       int    `json:"round"`
	Executor    string `json:"executor"`
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	ToolVersion string `json:"tool_version"`
	WallS       int    `json:"wall_s"`
	Conclusion  string `json:"conclusion"`
	OutOfWork   bool   `json:"out_of_workspace"`
	Text        string `json:"text"`
}

// runKey is one run of one item.
type runKey struct {
	item string
	run  int
}

var toolVersionRE = regexp.MustCompile(`^[^\x00-\x1f\x7f]{1,100}$`)

// maxExamAnswers bounds a part.
const maxExamAnswers = 200

// checkSubmission refuses a part that does not read as one, before anything
// is looked up.
func checkSubmission(s ExamSubmission) error {
	switch {
	case !sha256RE.MatchString(s.RunID):
		return invalid("run_id is the paper's sha256, as the paper was handed out")
	case !s.PinsOK:
		return invalid("pins_ok is not true: a run without the seat's pins is not an exam run")
	case !strings.HasPrefix(s.Executor, "exec:") || s.Executor == "exec:deterministic":
		return invalid("executor %q is not an agent executor", s.Executor)
	case s.Provider == "" || !modelRE.MatchString(s.Provider) || !modelRE.MatchString(s.Model):
		return invalid("provider and model are short names")
	case !toolVersionRE.MatchString(s.ToolVersion):
		return invalid("tool_version is one line of 1 to 100 bytes")
	case !sha256RE.MatchString(s.ExamSHA256):
		return invalid("exam_sha256 is the hex sha256 of the question set")
	case len(s.Answers) == 0 || len(s.Answers) > maxExamAnswers:
		return invalid("a part has 1 to %d answers", maxExamAnswers)
	}
	seen := map[runKey]bool{}
	for _, a := range s.Answers {
		k := runKey{a.Item, a.RunNo}
		switch {
		case !questionIDRE.MatchString(a.Item) || a.RunNo < 1 || seen[k]:
			return invalid("answer %q run %d: not an item and a run number, or given twice", a.Item, a.RunNo)
		case a.Round < 1:
			return invalid("answer %s run %d: round is 1 or more", a.Item, a.RunNo)
		case a.WallS < 0 || a.WallS > 86400:
			return invalid("answer %s run %d: wall_s is 0 to 86400", a.Item, a.RunNo)
		case a.Conclusion != "done" && a.Conclusion != "failed" && a.Conclusion != "none":
			return invalid("answer %s run %d: conclusion %q is not done, failed or none", a.Item, a.RunNo, a.Conclusion)
		}
		seen[k] = true
	}
	return nil
}

// examClock takes the exams' lock, then reads the clock (later moves it, for
// a test that needs a paper to lapse).
func examClock(ctx context.Context, tx pgx.Tx, later time.Duration) (time.Time, error) {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, examLock); err != nil {
		return time.Time{}, err
	}
	var now time.Time
	err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.Add(later), err
}

// examExecutor refuses an executor that is not a registered agent executor.
func examExecutor(ctx context.Context, tx pgx.Tx, executor string) error {
	if !strings.HasPrefix(executor, "exec:") || executor == "exec:deterministic" {
		return invalid("executor %q is not an agent executor", executor)
	}
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM agentrt.actor WHERE actor_id = $1 AND actor_kind = 'executor' AND retired_at IS NULL`,
		executor).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return invalid("executor %s is not registered", executor)
	}
	return err
}

// IssueExam hands out a paper of req.Job's exam for req.Executor on
// req.Host: refused while the executor has a paper open (one at a time, so
// no run picks the best of several papers); papers that lapsed with answers
// are graded first.
func IssueExam(ctx context.Context, db DB, req ExamRequest, set ExamSet) (*ExamPaper, error) {
	return issueExam(ctx, db, req, set, 0)
}

func issueExam(ctx context.Context, db DB, req ExamRequest, set ExamSet, later time.Duration) (*ExamPaper, error) {
	if req.Host == "" || req.Token == "" {
		return nil, invalid("a paper is asked for by a host's token")
	}
	if len(examClasses[req.Job]) == 0 {
		return nil, invalid("job %q has no exam", req.Job)
	}
	items := set.Items(req.Job)
	if len(items) == 0 {
		return nil, invalid("the exam set has no question for job %s", req.Job)
	}
	for _, it := range items {
		if !ExamClass(req.Job, it.Class) {
			return nil, invalid("item %s: class %q is not one the %s exam scores", it.ID, it.Class, req.Job)
		}
	}
	var out *ExamPaper
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		now, err := examClock(ctx, tx, later)
		if err != nil {
			return err
		}
		if err = examExecutor(ctx, tx, req.Executor); err != nil {
			return err
		}
		var job bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.job WHERE job_id = $1)`, req.Job).Scan(&job); err != nil {
			return err
		}
		if !job {
			return invalid("job %s is not a job here: no rule made it", req.Job)
		}
		papers, err := ungradedPapers(ctx, tx, now)
		if err != nil {
			return err
		}
		for _, p := range papers {
			if now.Before(p.ExpiresAt) && p.Executor == req.Executor {
				return fmt.Errorf("%w: %s has paper %s open until %s", ErrState, req.Executor, p.RunID, p.ExpiresAt.UTC().Format(time.RFC3339))
			}
		}
		for _, p := range papers {
			if !now.Before(p.ExpiresAt) {
				if err = gradeLapsed(ctx, tx, set, p, now); err != nil {
					return err
				}
			}
		}
		t0 := now.UTC().Truncate(time.Minute)
		paper := &ExamPaper{Job: req.Job, SetSHA256: set.Digest(), Host: req.Host, Executor: req.Executor, Nonce: newULID(now),
			IssuedAt: now.UTC(), T0: t0, ExpiresAt: now.Add(ExamPaperLife).UTC()}
		runs := make([]int, ExamRuns)
		for i := range runs {
			runs[i] = i + 1
		}
		for _, it := range items {
			sp, rerr := set.Render(it.ID, t0)
			if rerr != nil {
				return fmt.Errorf("store: exam item %s: %w", it.ID, rerr)
			}
			rounds := 1
			if sp.Repo != "" {
				rounds = 2
			}
			paper.Items = append(paper.Items, ExamPaperItem{Item: it.ID, T0: sp.T0.UTC(), Runs: runs, Rounds: rounds, Repo: sp.Repo,
				BaseCommit: sp.BaseCommit, Spec: sp.Spec})
		}
		body, err := json.Marshal(paper)
		if err != nil {
			return err
		}
		id, err := PutArtifact(ctx, tx, "controller", "exam", "application/json", body)
		if err != nil {
			return err
		}
		if _, err = Append(ctx, tx, Event{Stream: ExamsStream, Type: "artifact_put", Actor: "controller",
			Body: map[string]any{"artifact_id": id, "artifact_kind": "exam", "size_bytes": len(body), "uploaded_by": req.Token}}); err != nil {
			return err
		}
		paper.RunID = strings.TrimPrefix(id, "sha256:")
		out = paper
		return nil
	})
	return out, err
}

// issued is a paper as the stream records it: the event that issued it.
type issued struct {
	*ExamPaper
	event int64
}

// paperOf reads a paper's body; the run's id is its sha256.
func paperOf(event int64, artifact string, body []byte) (issued, error) {
	var p ExamPaper
	if err := json.Unmarshal(body, &p); err != nil {
		return issued{}, fmt.Errorf("store: paper %s: %w", artifact, err)
	}
	p.RunID = strings.TrimPrefix(artifact, "sha256:")
	return issued{ExamPaper: &p, event: event}, nil
}

// paperSQL reads the papers the controller put on the exams stream.
const paperSQL = `SELECT e.event_id, e.body_json->>'artifact_id', b.body FROM agentrt.event e
  JOIN agentrt.artifact_blob b ON b.artifact_id = e.body_json->>'artifact_id'
  WHERE e.event_stream_id = $1 AND e.event_type = 'artifact_put' AND e.actor_id = 'controller'`

// ungradedPapers are the papers not graded that may still matter at now:
// those issued in the last two lives (an open one is among them) and those
// with answers (graded once they lapse). One that lapsed long ago with no
// answer is left alone.
func ungradedPapers(ctx context.Context, tx pgx.Tx, now time.Time) ([]issued, error) {
	rows, err := tx.Query(ctx, paperSQL+`
	  AND NOT EXISTS (SELECT 1 FROM agentrt.event g WHERE g.event_stream_id = $1 AND g.event_type = 'capability_scored'
	    AND g.cause_event_id = e.event_id)
	  AND (e.at > $2 OR EXISTS (SELECT 1 FROM agentrt.event a WHERE a.event_stream_id = $1 AND a.event_type = 'artifact_put'
	    AND a.cause_event_id = e.event_id))
	  ORDER BY e.seq`, ExamsStream, now.Add(-2*ExamPaperLife))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []issued
	for rows.Next() {
		var event int64
		var artifact string
		var body []byte
		if err = rows.Scan(&event, &artifact, &body); err != nil {
			return nil, err
		}
		p, perr := paperOf(event, artifact, body)
		if perr != nil {
			return nil, perr
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// findPaper is the paper of run id, or pgx.ErrNoRows.
func findPaper(ctx context.Context, tx pgx.Tx, runID string) (issued, error) {
	var event int64
	var artifact string
	var body []byte
	if err := tx.QueryRow(ctx, paperSQL+` AND e.body_json->>'artifact_id' = $2`, ExamsStream, "sha256:"+runID).
		Scan(&event, &artifact, &body); err != nil {
		return issued{}, err
	}
	return paperOf(event, artifact, body)
}

// taken is an answer as kept: its artifact and what it says.
type taken struct {
	artifact string
	examAnswer
}

// takenAnswers are the answers of the paper issued by event, by run.
func takenAnswers(ctx context.Context, tx pgx.Tx, event int64) (map[runKey]taken, error) {
	rows, err := tx.Query(ctx, `SELECT e.body_json->>'artifact_id', b.body FROM agentrt.event e
	  JOIN agentrt.artifact_blob b ON b.artifact_id = e.body_json->>'artifact_id'
	  WHERE e.event_stream_id = $1 AND e.event_type = 'artifact_put' AND e.cause_event_id = $2 ORDER BY e.seq`, ExamsStream, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[runKey]taken{}
	for rows.Next() {
		var t taken
		var body []byte
		if err = rows.Scan(&t.artifact, &body); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(body, &t.examAnswer); err != nil {
			return nil, fmt.Errorf("store: answer %s: %w", t.artifact, err)
		}
		out[runKey{t.Item, t.RunNo}] = t
	}
	return out, rows.Err()
}

// gradedRun is the record of the paper issued by event once it is graded,
// or nil.
func gradedRun(ctx context.Context, tx pgx.Tx, event int64) (map[string]any, error) {
	var id int64
	var body map[string]any
	err := tx.QueryRow(ctx, `SELECT event_id, body_json FROM agentrt.event WHERE event_stream_id = $1
	  AND event_type = 'capability_scored' AND cause_event_id = $2`, ExamsStream, event).Scan(&id, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"event_id": id, "rows": int(numberOf(body["rows"]))}, nil
}
