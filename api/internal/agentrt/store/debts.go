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
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
	"github.com/weitingzhao/bifrost-platform/api/internal/localpath"
)

// Technical debt as records of the runtime (R5:5.8-3a; the infra
// repository's agent-config/work/multi-agent/
// DESIGN-tech-debt-runtime-2026-11-01.md, the Owner took T1-T13 as
// recommended on 2026-11-01; migration 0011). A debt is filed with a number
// only this environment issues: TD-n on PROD, TDS-n on STG, TDD-n on DEV, by
// AGENTRT_ENV (the environment whose runtime role the pool is held to), and
// the database lets only platform_agentrt_prod write a TD- number (§2.4). A number is one more than the largest of its prefix or
// the counter's floor, whichever is larger, under one lock; the same
// idempotency key gives back the same number. Nothing is filed while the
// environment's debt-projects rule is not in force or does not read, and a
// debt's project is one the rule lists (T3).
//
// Texts are rows of debt_text, never artifacts, and an event holds ids,
// enums and hashes (§4.3); visibility is the routes' to apply. No text may
// name a local path (T13; the strict scan of a project marked text_scan:
// strict comes with the routes that write texts).
//
// Lock order, every debt write through the store: the key's lock, the
// writer's lock (ensureWriter), the number lock (742131: filing, the floor)
// or the debt graph lock (742132: an edge), the debt rows, the event
// streams, and last the parts' lock of one debt (742133, hashtext(debt):
// taken by 0011's guard when a part order is written, after its event has
// locked the debt's stream). A writer past the store that writes an edge's
// event before the edge takes the stream before the graph lock: against a
// store write of an edge on the same debt that is a deadlock PostgreSQL
// breaks by refusing one of them; no store write takes the graph and the
// parts' lock together.

// The locks of the debt writes: numbering and the waiting graph (0011's
// guard takes the second one too).
const (
	debtNumberLock = 742131
	debtGraphLock  = 742132
)

// DebtCounterStream is the stream of the numbering floor's events.
const DebtCounterStream = "debt-counter"

// DebtStream is the event stream of a debt's history.
func DebtStream(id string) string { return "debt:" + id }

// debtPrefixes are the prefixes each environment issues (T2).
var debtPrefixes = map[string]string{"prod": "TD", "stg": "TDS", "dev": "TDD"}

// DebtPrefix is the prefix env issues: only PROD issues TD-.
func DebtPrefix(env string) (string, error) {
	p, ok := debtPrefixes[env]
	if !ok {
		return "", invalid("AGENTRT_ENV %q is not dev, stg or prod: no debt number is issued", env)
	}
	return p, nil
}

// Bounds of what a filing carries (the design leaves the counts open: 20
// texts, 50 pieces of evidence, 12 parts, 32 tags, 20 acceptance items).
const (
	maxDebtTitle     = 400
	maxDebtTextBytes = 256 << 10
	maxDebtTexts     = 20
	maxDebtEvidence  = 50
	maxDebtParts     = 12
	maxDebtTags      = 32
)

var (
	debtTagRE  = regexp.MustCompile(`^[a-z][a-z0-9:-]{0,63}$`)
	debtRepoRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)
	shortSHARE = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// ErrCycle: a blocks or part_of link would make the waiting graph circular;
// CycleError says how (§5.5, 409).
var ErrCycle = errors.New("store: the link would close a cycle")

// DebtText is one text of a debt; Heading keeps a free heading as written.
type DebtText struct{ Kind, Heading, Body string }

// DebtEvidence is one piece of evidence; a command is recorded, never run.
type DebtEvidence struct {
	Kind, Repo, Path string
	LineFrom, LineTo int
	Commit, Quote    string
	Command          string
	ObservedAt       time.Time
}

// DebtFiling is a new debt (file_debt): Repos are its parts, one per
// repository in the order given; with Acceptance and at least one part it
// flows automatically, else by hand (§6.1).
type DebtFiling struct {
	Project, Area, Title, Priority string
	Cost, Risk, Visibility         string
	Tags                           []string
	Texts                          []DebtText
	Evidence                       []DebtEvidence
	Repos                          []string
	Acceptance                     []any
}

func (f DebtFiling) request(env string) map[string]any {
	texts := make([]any, 0, len(f.Texts))
	for _, t := range f.Texts {
		texts = append(texts, map[string]any{"kind": t.Kind, "heading": t.Heading, "body": t.Body})
	}
	ev := make([]any, 0, len(f.Evidence))
	for _, e := range f.Evidence {
		ev = append(ev, e.request())
	}
	acc := f.Acceptance
	if acc == nil {
		acc = []any{}
	}
	return map[string]any{"env": env, "project": f.Project, "area": f.Area, "title": f.Title, "priority": f.Priority, "cost": f.Cost,
		"risk": f.Risk, "visibility": f.Visibility, "tags": noStrings(f.Tags), "texts": texts, "evidence": ev, "repos": noStrings(f.Repos),
		"acceptance": acc}
}

func (e DebtEvidence) request() map[string]any {
	return map[string]any{"kind": e.Kind, "repo": e.Repo, "path": e.Path, "line_from": e.LineFrom, "line_to": e.LineTo,
		"commit": e.Commit, "quote": e.Quote, "command": e.Command, "observed_at": timeOr(e.ObservedAt)}
}

// debtWriter checks a debt writer (§4.2): a crew thread or seat, the Owner
// or the runtime; never an external agent or a host (0011 refuses them too).
func debtWriter(w Writer) error {
	switch kindOf(w.Actor) {
	case "thread", "seat", "owner", "controller":
		return nil
	}
	return invalid("%s does not write debt: the crew's threads and seats, the Owner and the runtime do", w.Actor)
}

// checkDebtText checks a text: UTF-8, not blank, within bounds, no local
// path (T13), of a kind a writer may give.
func checkDebtText(what, s string, maxBytes int) error {
	switch {
	case !utf8.ValidString(s) || strings.ContainsRune(s, 0):
		return invalid("%s is not UTF-8 text", what)
	case strings.TrimSpace(s) == "":
		return invalid("%s is empty", what)
	case len(s) > maxBytes:
		return invalid("%s is %d bytes, more than %d", what, len(s), maxBytes)
	}
	if p, found := localpath.Find([]byte(s)); found {
		return invalid("%s names a local path (%s): debt texts name repository paths only", what, p)
	}
	return nil
}

func checkDebtTexts(texts []DebtText, refused ...string) error {
	if len(texts) > maxDebtTexts {
		return invalid("%d texts, more than %d", len(texts), maxDebtTexts)
	}
	for i, t := range texts {
		if !slices.Contains(debtTextKinds, t.Kind) || slices.Contains(refused, t.Kind) {
			return invalid("text %d is of kind %q, not one this write gives", i+1, t.Kind)
		}
		if t.Heading != "" {
			if utf8.RuneCountInString(t.Heading) > 200 {
				return invalid("the heading of text %d is longer than 200 characters", i+1)
			}
			if err := checkDebtText(fmt.Sprintf("the heading of text %d", i+1), t.Heading, 800); err != nil {
				return err
			}
		}
		if err := checkDebtText(fmt.Sprintf("text %d", i+1), t.Body, maxDebtTextBytes); err != nil {
			return err
		}
	}
	return nil
}

func checkDebtEvidence(ev []DebtEvidence) error {
	if len(ev) > maxDebtEvidence {
		return invalid("%d pieces of evidence, more than %d", len(ev), maxDebtEvidence)
	}
	for i, e := range ev {
		what := fmt.Sprintf("evidence %d", i+1)
		switch {
		case !slices.Contains(debtEvidenceKinds, e.Kind):
			return invalid("%s is of kind %q, not one of %v", what, e.Kind, debtEvidenceKinds)
		case e.Kind == "code_ref" && (e.Repo == "" || e.Path == ""):
			return invalid("%s points at code: it names a repository and a path", what)
		case e.Repo != "" && !debtRepoRE.MatchString(e.Repo):
			return invalid("%s: %q is not a repository name", what, e.Repo)
		case e.LineFrom < 0 || (e.LineTo != 0 && (e.LineFrom == 0 || e.LineTo < e.LineFrom)):
			return invalid("%s: lines %d to %d are not a range", what, e.LineFrom, e.LineTo)
		case e.Commit != "" && !shortSHARE.MatchString(e.Commit):
			return invalid("%s: %q is not a commit", what, e.Commit)
		case utf8.RuneCountInString(e.Quote) > 400 || utf8.RuneCountInString(e.Command) > 4000:
			return invalid("%s: a quote is at most 400 characters and a command 4000", what)
		case e.Kind != "code_ref" && e.Quote == "" && e.Command == "" && e.Path == "" && e.Commit == "":
			return invalid("%s says nothing: give a path, a commit, a quote or a command", what)
		}
		for part, s := range map[string]string{"path": e.Path, "quote": e.Quote, "command": e.Command} {
			if s != "" {
				if err := checkDebtText(what+"'s "+part, s, 4000); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (f DebtFiling) check() error {
	switch {
	case !productRE.MatchString(f.Project):
		return invalid("%q is not a project key", f.Project)
	case !slices.Contains(debtPriorities, f.Priority):
		return invalid("priority is one of %v, not %q", debtPriorities, f.Priority)
	case f.Cost != "" && !slices.Contains(debtCosts, f.Cost):
		return invalid("cost is one of %v, not %q", debtCosts, f.Cost)
	case f.Risk != "" && !slices.Contains(debtRisks, f.Risk):
		return invalid("risk is one of %v, not %q", debtRisks, f.Risk)
	case f.Visibility != "" && !slices.Contains(debtVisibilities, f.Visibility):
		return invalid("visibility is one of %v, not %q", debtVisibilities, f.Visibility)
	case len(f.Evidence) == 0:
		return invalid("a debt is filed with at least one piece of evidence (§6.2)")
	case len(f.Repos) > maxDebtParts:
		return invalid("%d parts, more than %d", len(f.Repos), maxDebtParts)
	}
	if err := checkDebtTitle(f.Title, f.Area); err != nil {
		return err
	}
	if err := checkDebtTags(f.Tags); err != nil {
		return err
	}
	for i, r := range f.Repos {
		if !debtRepoRE.MatchString(r) {
			return invalid("part %d: %q is not a repository name", i+1, r)
		}
	}
	if err := checkAcceptance(f.Acceptance); err != nil {
		return err
	}
	if err := checkDebtTexts(f.Texts, "import_raw", "reject_reason", "watch"); err != nil {
		return err
	}
	return checkDebtEvidence(f.Evidence)
}

func checkDebtTitle(title, area string) error {
	if err := checkDebtText("the title", title, 4*maxDebtTitle); err != nil {
		return err
	}
	if n := utf8.RuneCountInString(title); n > maxDebtTitle {
		return invalid("the title is %d characters, more than %d", n, maxDebtTitle)
	}
	if area == "" {
		return nil
	}
	if utf8.RuneCountInString(area) > 60 {
		return invalid("an area is 1 to 60 characters")
	}
	return checkDebtText("the area", area, 240)
}

func checkDebtTags(tags []string) error {
	if len(tags) > maxDebtTags {
		return invalid("%d tags, more than %d", len(tags), maxDebtTags)
	}
	for _, t := range tags {
		if !debtTagRE.MatchString(t) {
			return invalid("tag %q is not lower-case words (wave:<n>, theme:<slug>, look-first)", t)
		}
	}
	return nil
}

// debtRule is the debt-projects rule in force, or ErrState: nothing is
// written to debt without it (§2.4, as rules/intake.go).
func debtRule(ctx context.Context, tx pgx.Tx) (*rules.DebtProjects, error) {
	r, err := inForce(ctx, tx, rules.KindDebtProjects)
	if err != nil {
		return nil, err
	}
	if r == nil || r.DebtProjects == nil {
		return nil, fmt.Errorf("%w: no debt-projects rule is in force and reads: no debt is written", ErrState)
	}
	return r.DebtProjects, nil
}

// debtProjects is project as the rule in force lists it.
func debtProjects(ctx context.Context, tx pgx.Tx, project string) (*rules.DebtProject, error) {
	r, err := debtRule(ctx, tx)
	if err != nil {
		return nil, err
	}
	if p := r.Project(project); p != nil {
		return p, nil
	}
	return nil, invalid("project %q is not one the debt-projects rule lists", project)
}

// FileDebt files a debt in env (AGENTRT_ENV): the next number of env's
// prefix above the floor, open, with its texts, evidence and parts, by one
// debt_filed. The same key with the same request answers the same number.
func FileDebt(ctx context.Context, db DB, env string, w Writer, key string, f DebtFiling) (map[string]any, error) {
	prefix, err := DebtPrefix(env)
	if err != nil {
		return nil, err
	}
	if err := debtWriter(w); err != nil {
		return nil, err
	}
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if err := f.check(); err != nil {
		return nil, err
	}
	return idempotentAs(ctx, db, w, "debt-file", key, f.request(env), nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if _, err := debtProjects(ctx, tx, f.Project); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, debtNumberLock); err != nil {
			return nil, err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT GREATEST(COALESCE(max(substr(debt_id, $2)::int), 0), (SELECT floor FROM agentrt.debt_counter)) + 1
		  FROM agentrt.debt WHERE debt_id ~ $1`, "^"+prefix+"-[0-9]+$", len(prefix)+2).Scan(&n); err != nil {
			return nil, err
		}
		id := fmt.Sprintf("%s-%d", prefix, n)
		flow := "manual"
		if len(f.Acceptance) > 0 && len(f.Repos) > 0 {
			flow = "auto"
		}
		texts, err := nextDebtIDs(ctx, tx, "debt_text", len(f.Texts))
		if err != nil {
			return nil, err
		}
		evs, err := nextDebtIDs(ctx, tx, "debt_evidence", len(f.Evidence))
		if err != nil {
			return nil, err
		}
		parts, err := nextDebtIDs(ctx, tx, "debt_part", len(f.Repos))
		if err != nil {
			return nil, err
		}
		body := map[string]any{"source": "file", "project": f.Project, "priority": f.Priority, "state": "open", "flow": flow}
		for k, ids := range map[string][]int64{"debt_text_ids": texts, "debt_evidence_ids": evs, "debt_part_ids": parts} {
			if len(ids) > 0 {
				body[k] = anyInts(ids)
			}
		}
		if _, err = appendDebt(ctx, tx, "debt_filed", w.Actor, id, "", "", body); err != nil {
			return nil, err
		}
		acc, err := canon.Encode(orEmpty(f.Acceptance))
		if err != nil {
			return nil, err
		}
		visibility := f.Visibility
		if visibility == "" {
			visibility = "project"
		}
		var specBy any // who wrote the spec: its filer, when it has one (0011)
		if len(f.Acceptance) > 0 {
			specBy = w.Actor
		}
		if _, err := tx.Exec(ctx, `INSERT INTO agentrt.debt (debt_id, project, area, title, priority, state, flow, cost, risk, visibility, tags,
		    acceptance_json, acceptance_by, filed_at, state_since, created_by_actor_id)
		  VALUES ($1,$2,$3,$4,$5,'open',$6,$7,$8,$9,$10,$11::jsonb,$12,clock_timestamp(),clock_timestamp(),$13)`,
			id, f.Project, nullable(f.Area), f.Title, f.Priority, flow, nullable(f.Cost), nullable(f.Risk), visibility, noStrings(f.Tags),
			string(acc), specBy, w.Actor); err != nil {
			return nil, err
		}
		for i, t := range f.Texts {
			if err := insertDebtText(ctx, tx, texts[i], id, w.Actor, t); err != nil {
				return nil, err
			}
		}
		for i, e := range f.Evidence {
			if err := insertDebtEvidence(ctx, tx, evs[i], id, w.Actor, e); err != nil {
				return nil, err
			}
		}
		for i, repo := range f.Repos {
			if _, err := tx.Exec(ctx, `INSERT INTO agentrt.debt_part (debt_part_id, debt_id, repo, ordinal) OVERRIDING SYSTEM VALUE
			  VALUES ($1,$2,$3,$4)`, parts[i], id, repo, i+1); err != nil {
				return nil, err
			}
		}
		return debtWritten(ctx, tx, id)
	})
}

func orEmpty(l []any) []any {
	if l == nil {
		return []any{}
	}
	return l
}

func noStrings(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

// nextDebtIDs takes n ids from a debt table's identity sequence, so the
// event written first can name the rows inserted after it.
func nextDebtIDs(ctx context.Context, tx pgx.Tx, table string, n int) ([]int64, error) {
	if !strings.HasPrefix(table, "debt_") {
		return nil, fmt.Errorf("store: %s takes no ids ahead", table)
	}
	if n == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `SELECT nextval(pg_get_serial_sequence($1, $2)) FROM generate_series(1, $3)`, "agentrt."+table, table+"_id", n)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func insertDebtText(ctx context.Context, tx pgx.Tx, id int64, debt, actor string, t DebtText) error {
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.debt_text (debt_text_id, debt_id, text_kind, heading, body, sha256, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,$4,$5,$6,$7)`, id, debt, t.Kind, nullable(t.Heading), t.Body, textSHA(t.Body), actor)
	return err
}

func insertDebtEvidence(ctx context.Context, tx pgx.Tx, id int64, debt, actor string, e DebtEvidence) error {
	zero := func(n int) any {
		if n == 0 {
			return nil
		}
		return n
	}
	var observed any
	if !e.ObservedAt.IsZero() {
		observed = e.ObservedAt
	}
	_, err := tx.Exec(ctx, `INSERT INTO agentrt.debt_evidence (debt_evidence_id, debt_id, evidence_kind, repo, path, line_from, line_to,
	    commit_sha, quote, command, observed_at, created_by_actor_id)
	  OVERRIDING SYSTEM VALUE VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, debt, e.Kind, nullable(e.Repo), nullable(e.Path),
		zero(e.LineFrom), zero(e.LineTo), nullable(e.Commit), nullable(e.Quote), nullable(e.Command), observed, actor)
	return err
}

// appendDebt appends a debt event to the debt's stream (the body names
// it); mission and step fill the envelope when the row written has them.
func appendDebt(ctx context.Context, tx pgx.Tx, typ, actor, debt, mission, step string, body map[string]any) (Appended, error) {
	body["debt_id"] = debt
	return Append(ctx, tx, Event{Stream: DebtStream(debt), Type: typ, Actor: actor, MissionID: mission, PlanNode: step, Body: body})
}

// DebtView is a debt's record without its texts.
type DebtView struct {
	ID, Project, Area, Title, Priority, State, Conclusion, DuplicateOf, Flow string
	Cost, Risk, Visibility, CreatedBy                                        string
	Tags                                                                     []string
	Acceptance                                                               json.RawMessage
	WatchUntil, FiledAt, StateSince                                          time.Time
	Version                                                                  int
}

// Doc is the view as a response has it.
func (v DebtView) Doc() map[string]any {
	d := map[string]any{"debt_id": v.ID, "project": v.Project, "title": v.Title, "priority": v.Priority, "state": v.State,
		"flow": v.Flow, "visibility": v.Visibility, "tags": noStrings(v.Tags), "filed_at": timeText(v.FiledAt),
		"state_since": timeText(v.StateSince), "created_by": v.CreatedBy, "version": v.Version}
	for k, s := range map[string]string{"area": v.Area, "conclusion": v.Conclusion, "duplicate_of": v.DuplicateOf, "cost": v.Cost, "risk": v.Risk} {
		if s != "" {
			d[k] = s
		}
	}
	if !v.WatchUntil.IsZero() {
		d["watch_until"] = timeText(v.WatchUntil)
	}
	if acc, err := canon.Parse(v.Acceptance); err == nil {
		d["acceptance"] = acc
	}
	return d
}

const debtColumns = `debt_id, project, COALESCE(area, ''), title, priority, state, COALESCE(conclusion, ''), COALESCE(duplicate_of, ''),
  flow, COALESCE(cost, ''), COALESCE(risk, ''), visibility, created_by_actor_id, tags, acceptance_json::text,
  COALESCE(watch_until, 'epoch'::timestamptz), filed_at, state_since, version`

func scanDebt(row pgx.Row) (DebtView, error) {
	var v DebtView
	var acc string
	err := row.Scan(&v.ID, &v.Project, &v.Area, &v.Title, &v.Priority, &v.State, &v.Conclusion, &v.DuplicateOf, &v.Flow, &v.Cost,
		&v.Risk, &v.Visibility, &v.CreatedBy, &v.Tags, &acc, &v.WatchUntil, &v.FiledAt, &v.StateSince, &v.Version)
	if v.WatchUntil.Equal(time.Unix(0, 0)) {
		v.WatchUntil = time.Time{}
	}
	v.Acceptance = json.RawMessage(acc)
	return v, err
}

func readDebt(ctx context.Context, tx pgx.Tx, id string, lock bool) (DebtView, error) {
	if !debtIDRE.MatchString(id) {
		return DebtView{}, invalid("%q is not a debt id (TD-<n>, TDS-<n>, TDD-<n>)", id)
	}
	q := `SELECT ` + debtColumns + ` FROM agentrt.debt WHERE debt_id = $1`
	if lock {
		q += ` FOR UPDATE`
	}
	v, err := scanDebt(tx.QueryRow(ctx, q, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return v, invalid("there is no debt %s", id)
	}
	return v, err
}

func debtWritten(ctx context.Context, tx pgx.Tx, id string) (map[string]any, error) {
	v, err := readDebt(ctx, tx, id, false)
	if err != nil {
		return nil, err
	}
	return v.Doc(), nil
}

// DebtState is a debt's record as it is now, without its texts.
func DebtState(ctx context.Context, db DB, id string) (DebtView, error) {
	var v DebtView
	err := readOnly(ctx, db, func(tx pgx.Tx) error {
		var err error
		v, err = readDebt(ctx, tx, id, false)
		return err
	})
	return v, err
}
