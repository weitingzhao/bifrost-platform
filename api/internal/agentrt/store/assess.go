package store

import (
	"context"
	"time"
)

// AI assessments are advice only. TD-463: the Owner first opts in on the
// item's stream; the controller then gives the existing read-only brief to
// the chief of staff. Its evidence, limits and acceptance rules are unchanged.
// Legacy asks and results remain valid and are never dispatched again.

// The kinds of item assessed.
const (
	AssessCard     = "card"
	AssessApproval = "approval"
)

const (
	// AssessSuccess is an assessment's mission's success criterion.
	AssessSuccess = "the chief of staff's assessment is recorded"
	// AssessOffPlanReason is why an assessment is off the plan tree.
	AssessOffPlanReason = "an assessment of a card or an approval for the Owner: advice beside the rule's, never an answer"
	// AssessTimeout is how long an assessment may take from its ask; one not
	// in by then is recorded unavailable and its task cancelled.
	AssessTimeout = 45 * time.Minute
	// AssessPerPass bounds the assessments one pass asks for, so a backlog
	// (the first pass after a release) comes over several passes.
	AssessPerPass = 3
	// AssessNeed is the need an assessment task carries, for a capability
	// fact to name; assessClass its task class.
	AssessNeed  = "assessment"
	assessClass = "assessment"
)

// AssessStream is the stream of an item's assessment: its ask, then its
// assessment or why there is none.
func AssessStream(kind, id string) string { return "assess:" + kind + ":" + id }

// AssessOption is one option of an item: a card's, or an approval's approve
// and reject.
type AssessOption struct{ Key, Label, Detail string }

// AssessItem is a card or an approval as the Owner sees it.
type AssessItem struct {
	Kind, ID    string // AssessCard and the question's id, or AssessApproval and its number
	Title, Body string
	Options     []AssessOption
	Recommended string // the option the card itself recommends (the runtime's rule), "" for none
	TaskID      string // the runtime task a card is about
	CreatedAt   time.Time
	Record      map[string]any // an approval's fields as the Owner reads them
	Repos       []string       // repositories an approval names
	Others      []AssessOther  // other approvals open now
}

// AssessOther is another open approval, as context.
type AssessOther struct {
	Number         int
	Action, Status string
	Summary        string
	CreatedAt      time.Time
}

// EvidenceQuery is what an assessment reads outside the runtime: the commits
// on main of Repos back to Since that are results of Tasks, name one of Items
// in their Work trailer, or are one of Commits; and Items in the ledger.
type EvidenceQuery struct {
	Repos, Tasks, Items, Commits []string
	Since                        time.Time
}

// EvidenceCommit is a commit on main.
type EvidenceCommit struct {
	Repo, SHA, Subject string
	At                 time.Time
	ChangeID, Task     string
	Work               []string
}

// LedgerItem is an item of the ledger: its id (a heading), title and state.
type LedgerItem struct{ ID, Title, State string }

// Evidence is what the platform read for an EvidenceQuery: Org is the
// organization the repositories are read in, Ledger where the ledger is;
// NotRead says what could not be read.
type Evidence struct {
	Org                    string
	Commits                []EvidenceCommit
	Ledger                 []LedgerItem
	LedgerRepo, LedgerPath string
	NotRead                []string
}

// EvidenceReader reads Evidence; it writes nothing.
type EvidenceReader func(ctx context.Context, q EvidenceQuery) Evidence
