// Package debtimport parses the frozen debt ledgers without performing I/O or
// executing acceptance commands. Requests are proposals, not database writes.
package debtimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

const Format = "agentrt.debt_import/v1"

// History is supplied by the caller; the package never invokes git.
type History interface {
	FirstSeen(td string) (time.Time, error)
}

type Project struct {
	Key  string   `json:"key"`
	Owns []string `json:"owns"`
}

// Options fixes the import instant so retries are byte-for-byte reproducible.
// Projects is a caller-supplied ownership snapshot, not an active signed rule.
type Options struct {
	ImportedAt   time.Time
	History      History
	Projects     []Project
	CrewProject  string
	RepoRoots    map[string][]string
	FloorSources []FloorSource
}

type Source struct {
	ImportedAt     time.Time           `json:"imported_at"`
	LedgerSHA256   string              `json:"ledger_sha256"`
	RatchetsSHA256 string              `json:"ratchets_sha256"`
	Projects       []Project           `json:"projects"`
	RepoRoots      map[string][]string `json:"repo_roots,omitempty"`
}

// Request uses the design's table and column names. Identity numbers for texts,
// parts and decisions are request-local handles; the store must remap them.
// Markdown and project_notes/last_seen are transport metadata, not new tables.
type Request struct {
	Format       string          `json:"format"`
	Source       Source          `json:"source"`
	Floor        int             `json:"floor"`
	FloorSources []FloorSource   `json:"floor_sources"`
	SourceTexts  []SourceText    `json:"source_text"`
	Debts        []Debt          `json:"debt"`
	Texts        []Text          `json:"debt_text"`
	Evidence     []Evidence      `json:"debt_evidence"`
	Parts        []Part          `json:"debt_part"`
	Edges        []Edge          `json:"debt_edge"`
	Refs         []Ref           `json:"debt_ref"`
	Runs         []AcceptanceRun `json:"debt_acceptance_run"`
	Decisions    []Decision      `json:"debt_decision"`
	Ratchets     []Ratchet       `json:"ratchet"`
	Gaps         []Gap           `json:"accepted_gap"`
	ProjectNotes []ProjectNote   `json:"project_notes"`
	LastSeen     []LastSeen      `json:"last_seen"`
	// Umbrellas need an allocated id at import time; they never consume a TD
	// number during parsing. Edges name this local handle in to_debt_id.
	Umbrellas []Umbrella `json:"umbrellas"`
	Markdown  Layout     `json:"markdown"`
}

type Debt struct {
	DebtID         string            `json:"debt_id"`
	Project        string            `json:"project,omitempty"`
	Area           string            `json:"area"`
	Title          string            `json:"title"`
	Priority       string            `json:"priority"`
	State          string            `json:"state,omitempty"`
	Flow           string            `json:"flow"`
	WatchUntil     *time.Time        `json:"watch_until,omitempty"`
	WatchTextID    int               `json:"watch_text_id,omitempty"`
	Cost           string            `json:"cost,omitempty"`
	Risk           string            `json:"risk,omitempty"`
	Visibility     string            `json:"visibility"`
	Tags           []string          `json:"tags"`
	AcceptanceJSON []json.RawMessage `json:"acceptance_json"`
	FiledAt        *time.Time        `json:"filed_at"`
	StateSince     time.Time         `json:"state_since"`
}

type Text struct {
	DebtTextID int    `json:"debt_text_id"`
	DebtID     string `json:"debt_id"`
	TextKind   string `json:"text_kind"`
	Heading    string `json:"heading,omitempty"`
	Body       string `json:"body"`
	SHA256     string `json:"sha256"`
}

type Evidence struct {
	DebtID       string `json:"debt_id"`
	EvidenceKind string `json:"evidence_kind"`
	Repo         string `json:"repo,omitempty"`
	Path         string `json:"path,omitempty"`
	LineFrom     int    `json:"line_from,omitempty"`
	LineTo       int    `json:"line_to,omitempty"`
	CommitSHA    string `json:"commit_sha,omitempty"`
	Quote        string `json:"quote,omitempty"`
}

type Part struct {
	DebtPartID int    `json:"debt_part_id"`
	DebtID     string `json:"debt_id"`
	Repo       string `json:"repo"`
	Ordinal    int    `json:"ordinal"`
	ApplyGate  string `json:"apply_gate"`
}
type Edge struct {
	EdgeKind   string `json:"edge_kind"`
	FromDebtID string `json:"from_debt_id"`
	ToDebtID   string `json:"to_debt_id"`
}
type Ref struct {
	DebtID         string `json:"debt_id"`
	RefKind        string `json:"ref_kind"`
	RatchetID      string `json:"ratchet_id,omitempty"`
	DebtDecisionID int    `json:"debt_decision_id,omitempty"`
	Covers         string `json:"covers,omitempty"`
	DebtPartID     int    `json:"debt_part_id,omitempty"`
	ApprovalNo     int    `json:"approval_no,omitempty"`
}
type AcceptanceRun struct {
	DebtID       string    `json:"debt_id"`
	Runner       string    `json:"runner"`
	SourceSHA256 string    `json:"source_sha256"`
	Result       string    `json:"result"`
	RanAt        time.Time `json:"ran_at"`
	// An unassigned key preserves hashes whose repo the source does not name.
	AtCommits map[string][]string `json:"at_commits"`
}
type Decision struct {
	DebtDecisionID int    `json:"debt_decision_id"`
	Title          string `json:"title"`
	BodyText       string `json:"body_text"`
	State          string `json:"state"`
}
type Ratchet struct {
	RatchetID     string   `json:"ratchet_id"`
	Name          string   `json:"name"`
	Location      string   `json:"location"`
	Guards        string   `json:"guards"`
	Strength      string   `json:"strength,omitempty"`
	ScopeGaps     string   `json:"scope_gaps,omitempty"`
	StrengthClass string   `json:"strength_class"`
	TDRefs        []string `json:"td_refs"`
	State         string   `json:"state"`
}
type Gap struct {
	AcceptedGapID string `json:"accepted_gap_id"`
	Project       string `json:"project,omitempty"`
	GapKind       string `json:"gap_kind"`
	Title         string `json:"title"`
	Reason        string `json:"reason"`
	ReopenWhen    string `json:"reopen_when"`
	State         string `json:"state"`
}
type ProjectNote struct {
	Project string `json:"project,omitempty"`
	Heading string `json:"heading"`
	Body    string `json:"body"`
}
type LastSeen struct {
	Project string `json:"project,omitempty"`
	Commit  string `json:"commit"`
	Body    string `json:"body"`
}
type Umbrella struct {
	Handle string `json:"handle"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

// FloorSource records a checked maximum, including zero for a checked empty source.
type FloorSource struct {
	Kind       string `json:"kind"`
	Repository string `json:"repository,omitempty"`
	Ref        string `json:"ref,omitempty"`
	DebtID     string `json:"debt_id,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

// SourceText holds document prose without a debt FK. It is import metadata.
type SourceText struct {
	TextKind string `json:"text_kind"`
	Body     string `json:"body"`
}

type SourceDetail struct {
	Document string   `json:"document"`
	Start    int      `json:"start"`
	End      int      `json:"end"`
	Line     int      `json:"line"`
	Paths    []string `json:"paths"`
	Original string   `json:"original"`
	Rendered string   `json:"rendered"`
}

type Finding struct {
	Item     string        `json:"item"`
	Rule     int           `json:"rule"`
	Category string        `json:"category"`
	Detail   string        `json:"detail"`
	Source   *SourceDetail `json:"source,omitempty"`
}
type Report struct {
	Findings     []Finding      `json:"findings"`
	Counts       map[string]int `json:"counts"`
	Distribution Distribution   `json:"distribution"`
}
type Distribution struct {
	State    map[string]int `json:"state"`
	Priority map[string]int `json:"priority"`
	Project  map[string]int `json:"project,omitempty"`
	Repo     map[string]int `json:"repo"`
}

func (r Request) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }
func (r Report) JSON() ([]byte, error)  { return json.MarshalIndent(r, "", "  ") }
func digest(s string) string            { b := sha256.Sum256([]byte(s)); return hex.EncodeToString(b[:]) }
func number(s string) int               { _, tail, _ := strings.Cut(s, "-"); n, _ := strconv.Atoi(tail); return n }
func sortedIDs(s []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, id := range s {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return number(out[i]) < number(out[j]) })
	return out
}
func (p *parser) finding(item string, rule int, category, detail string) {
	p.report.Findings = append(p.report.Findings, Finding{Item: item, Rule: rule, Category: category, Detail: detail})
}
func (p *parser) finishReport() {
	sort.SliceStable(p.report.Findings, func(i, j int) bool {
		a, b := p.report.Findings[i], p.report.Findings[j]
		if a.Item != b.Item {
			if strings.HasPrefix(a.Item, "TD-") && strings.HasPrefix(b.Item, "TD-") {
				return number(a.Item) < number(b.Item)
			}
			return a.Item < b.Item
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.Category != b.Category {
			return a.Category < b.Category
		}
		return a.Detail < b.Detail
	})
	p.report.Counts = map[string]int{}
	for _, f := range p.report.Findings {
		p.report.Counts[f.Category]++
	}
	p.report.Distribution = p.req.Distribution()
}
func (r Request) Distribution() Distribution {
	d := Distribution{map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}}
	for _, v := range r.Debts {
		d.State[v.State]++
		d.Priority[v.Priority]++
		d.Project[v.Project]++
	}
	for _, v := range r.Parts {
		d.Repo[v.Repo]++
	}
	return d
}
func (p *parser) addText(id, kind, heading, body string) int {
	if body == "" {
		return 0
	}
	n := len(p.req.Texts) + 1
	p.req.Texts = append(p.req.Texts, Text{n, id, kind, heading, body, digest(body)})
	return n
}
func pointer(table string, i int, field string) string {
	return fmt.Sprintf("/%s/%d/%s", table, i, field)
}
