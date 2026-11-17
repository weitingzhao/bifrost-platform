package eventlog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// The read shape of the ledger: the columns of agentrt.event another package
// may name when it reads the table in a projection of its own. Writing is
// this package's alone (Append); a question asked the same way in several
// places is a function here (Last, Has, HasBody); a projection that joins
// the ledger to a domain's tables stays that domain's SQL and may use these
// columns, as they are described, and no others.
//
// It is the shared-storage form of a domain's contract (the Owner's rule of
// 2026-11-16: domains speak through contracts; the coordinator's ruling the
// same day on the review's third finding). A column is added, removed,
// retyped or given another meaning only with ReadShapeVersion raised, and
// the packages that read it told: eventlog's own tests hold the declaration
// to the table the migrations build, and to the fingerprint recorded for the
// version.

// ReadShapeVersion is the version of ReadShape.
const ReadShapeVersion = 1

// Column is one column of the read shape.
type Column struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // as information_schema.columns.data_type names it
	Nullable bool   `json:"nullable"`
	Meaning  string `json:"meaning"`
}

// ReadShape is the read shape of agentrt.event, in the table's column order.
var ReadShape = []Column{
	{"event_id", "bigint", false, "the event's number; it rises with insertion, not with commit (order by txid, event_id for commit order)"},
	{"event_stream_id", "text", false, "the stream the event belongs to"},
	{"seq", "bigint", false, "the event's place in its stream, from 1, without gaps"},
	{"event_type", "text", false, "one of the closed vocabulary's types"},
	{"actor_id", "text", false, "who wrote it; a row of agentrt.actor"},
	{"mission_id", "text", true, "the mission it is about, when it is about one"},
	{"plan_node_id", "text", true, "the plan node it is about, when it is about one"},
	{"task_id", "text", true, "the task of the retired dispatch lane; history only, no new event sets it"},
	{"attempt_id", "text", true, "the attempt of the retired dispatch lane; history only, no new event sets it"},
	{"cause_event_id", "bigint", true, "the event this one answers, when it answers one"},
	{"body_json", "jsonb", false, "the body, as the vocabulary of its type declares it"},
	{"prev_sha256", "text", false, "the hash of the stream's previous event; the chain is checked by VerifyStream, not by a reader"},
	{"sha256", "text", false, "this event's hash over its envelope; the same"},
	{"txid", "bigint", false, "the transaction that wrote it; with event_id, the order events became visible in"},
	{"at", "timestamp with time zone", false, "when it was written, by the database's clock"},
}

// ReadShapeFingerprint is a stable fingerprint of the declared shape, the
// meanings included: a change of any of it changes the fingerprint.
func ReadShapeFingerprint() string {
	raw, _ := json.Marshal(ReadShape)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
