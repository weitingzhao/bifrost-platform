package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Claiming.

// Tool is one executor a host can run, with the model it would use.
type Tool struct {
	Executor string // actor id, e.g. exec:codex
	Provider string // model provider; required, independence is checked on it
	Model    string // may be empty until the executor starts
}

// ClaimRequest is a host's poll for work.
type ClaimRequest struct {
	HostID    string
	FreeSlots int
	Tools     []Tool // in the host's order of preference
	// Disk is the free disk the host reports, nil when it reports none (an
	// agentd from before TD-415); below DiskFloor (0: DefaultDiskFloor) the
	// claim leases nothing, as with no free slot (hostdisk.go).
	Disk      *HostDisk
	DiskFloor int64
	// Signals is the headroom the host reports, nil when it reports none (an
	// agentd from before elastic slots); kept for the slots pass
	// (hostslots.go).
	Signals *HostSignals
}

// Claim is a leased attempt and what agentd needs to run it.
type Claim struct {
	Title                                                                                 string // optional task title for the hand-in commit
	TaskID, AttemptID, MissionID, Role, Repo, SpecArtifact, BaseCommit, PushedRef, Effect string
	WorkID                                                                                string // the mission's, for the Work trailer
	Tool                                                                                  Tool
	AttemptNo                                                                             int
	Epoch                                                                                 int64
	HardLimit                                                                             time.Duration
	LeaseExpiresAt                                                                        time.Time
	// Failed are the checks the task's earlier rounds failed, for an
	// implementation in a round after the first (TD-405, claimfailed.go).
	Failed []FailedCheck
	// SubjectReport is the report the attempt a check checks handed in, and
	// SubjectEffect its task's effect class, for a check (TD-409,
	// claimsubject.go).
	SubjectReport, SubjectEffect string
	// GrantedSlots is how many attempts the host may hold at once now, the
	// lease included (hostslots.go): agentd asks for no more.
	GrantedSlots int
}

type candidate struct {
	id, mission, role, spec, subject, base, repo, effect string
	title                                                string
	envelope                                             string // the standing envelope its mission started under
	tenant                                               string // its mission's: whose routing rule applies
	class                                                string // its task class: which scores bear on it
	pin                                                  string // its executor pin, "" for none (pin.go)
	subjectPin                                           string // the pin of the task it checks, "" for none (route.go)
	core                                                 bool
	needs, route                                         []string
	limitS                                               int
	created                                              time.Time
}

// ConfirmLimit bounds the read-back of a claim whose commit failed.
const ConfirmLimit = 3 * time.Second

const candidateColumns = `SELECT t.task_id, t.mission_id, t.role, t.spec_artifact_id,
	  COALESCE(t.subject_task_id, ''), COALESCE(t.base_commit, ''), COALESCE(t.repo, ''), t.effect_class,
	  t.core OR COALESCE(s.core, false), ARRAY(SELECT jsonb_array_elements_text(t.needs_json)),
	  ARRAY(SELECT jsonb_array_elements_text(t.route_json)), t.hard_limit_s, t.created_at, COALESCE(m.envelope_rule_id, ''), m.tenant, t.task_class,
	  COALESCE(t.executor_pin, ''), COALESCE(s.executor_pin, ''), t.title
	  FROM agentrt.task t JOIN agentrt.mission m ON m.mission_id = t.mission_id
	  LEFT JOIN agentrt.task s ON s.task_id = t.subject_task_id`

func scanCandidate(row pgx.CollectableRow) (candidate, error) {
	var c candidate
	err := row.Scan(&c.id, &c.mission, &c.role, &c.spec, &c.subject, &c.base, &c.repo, &c.effect, &c.core, &c.needs, &c.route,
		&c.limitS, &c.created, &c.envelope, &c.tenant, &c.class, &c.pin, &c.subjectPin, &c.title)
	return c, err
}

// candidateOf reads one task as a candidate, whatever its state.
func candidateOf(ctx context.Context, tx pgx.Tx, taskID string) (candidate, error) {
	rows, err := tx.Query(ctx, candidateColumns+` WHERE t.task_id = $1`, taskID)
	if err != nil {
		return candidate{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanCandidate)
}
