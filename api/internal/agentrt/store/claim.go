package store

import (
	"time"
)

// Claiming.

// Tool is one executor a host can run, with the model it would use.
type Tool struct {
	Executor string // actor id, e.g. exec:codex
	Provider string // model provider; required, independence is checked on it
	Model    string // may be empty until the executor starts
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

// ConfirmLimit bounds the read-back of a claim whose commit failed.
const ConfirmLimit = 3 * time.Second
