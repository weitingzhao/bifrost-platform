package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// The architecture table on GET /progress (W-31 2.5 G2 A; design v5 §5.1.2
// 「arch_row 的勾 = 它的完成规则此刻的求值」). Each row's tick is its done rule
// evaluated in the projection's snapshot, the way the controller evaluates a
// step: from the facts recorded there and, for a gate, against what the
// environment runs as the controller last recorded it (deploy_seen), so a
// new deployment takes the tick away until the gate passes for it (T33). A
// row with no rule is unknown: its gate tests are not settled yet. Beside the
// tick, the row's latest note: what it actually is now, in words.

// ReasonNoGate is the reason of a row with no done rule.
const ReasonNoGate = "no gate test yet"

// ReasonNotGates is the reason of a row whose done rule names more than gate
// tests: a table row's tick is its gates against what runs now, never a fact
// that stays true once recorded (review 427 A2).
const ReasonNotGates = "its done rule is not gate tests only"

// archRow is a row of the architecture table as planView read it.
type archRow struct{ id, title string }

// archRowsDoc is the rows of an architecture root, each with its tick, the
// reason for it and its latest note. A read that fails is an error; a rule
// that does not parse is unknown, with why.
func archRowsDoc(ctx context.Context, tx pgx.Tx, rows []archRow, deploys *recordedDeploys, gates map[string]plan.Tri) ([]any, error) {
	out := make([]any, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	rules := map[string]string{}
	rrows, err := tx.Query(ctx, `SELECT plan_node_id, done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var id, doc string
		if err = rrows.Scan(&id, &doc); err != nil {
			rrows.Close()
			return nil, err
		}
		rules[id] = doc
	}
	rrows.Close()
	if err = rrows.Err(); err != nil {
		return nil, err
	}
	notes := map[string]map[string]any{}
	nrows, err := tx.Query(ctx, `SELECT DISTINCT ON (plan_node_id) plan_node_id, body_json->>'text', at, actor_id FROM agentrt.event
	  WHERE event_type = 'note' AND plan_node_id = ANY($1) ORDER BY plan_node_id, event_id DESC`, ids)
	if err != nil {
		return nil, err
	}
	for nrows.Next() {
		var id, text, by string
		var at time.Time
		if err = nrows.Scan(&id, &text, &at, &by); err != nil {
			nrows.Close()
			return nil, err
		}
		notes[id] = map[string]any{"text": text, "at": at.UTC().Format(time.RFC3339Nano), "by": by}
	}
	nrows.Close()
	if err = nrows.Err(); err != nil {
		return nil, err
	}
	// gates is the projection's: one reading of a gate whichever rows name it,
	// since what each environment runs is the same for every row (review 427 B2).
	for _, r := range rows {
		tick, reason, err := archTick(ctx, tx, r.id, rules[r.id], deploys, gates)
		if err != nil {
			return nil, err
		}
		var note any
		if n, ok := notes[r.id]; ok {
			note = n
		}
		out = append(out, map[string]any{"plan_node_id": r.id, "title": r.title, "tick": tick, "reason": reason, "note": note})
	}
	return out, nil
}

// archTick evaluates a row's done rule now: true, false or unknown, and the
// reason, which names the atoms that decided it.
func archTick(ctx context.Context, tx pgx.Tx, id, doc string, deploys *recordedDeploys, gates map[string]plan.Tri) (string, string, error) {
	rule, err := plan.Parse([]byte(doc))
	switch {
	case err != nil:
		return plan.Unknown.String(), "its done rule does not parse: " + err.Error(), nil
	case rule.Empty():
		return plan.Unknown.String(), ReasonNoGate, nil
	case !rule.OnlyStanding():
		return plan.Unknown.String(), ReasonNotGates, nil
	}
	facts := &stepFacts{ctx: ctx, tx: tx, step: id, deployed: deploys.read}
	var statusErr error
	seen := &explained{Facts: facts, gates: gates, status: func(env, test, spec string) string {
		if statusErr != nil {
			return ""
		}
		s, err := gateStatus(ctx, tx, deploys.get(env), test, spec)
		statusErr = err
		return s
	}}
	res := rule.Eval(seen)
	if facts.err != nil {
		return "", "", facts.err
	}
	if statusErr != nil {
		return "", "", statusErr
	}
	if deploys.err != nil {
		return "", "", deploys.err
	}
	return res.Value.String(), seen.reason(res.Value, deploys), nil
}

// recordedDeploys reads what each environment runs as the controller last
// recorded it (deploy_seen, store.SeeDeploy), once per environment per
// projection, so every row is checked against the same deployment.
type recordedDeploys struct {
	ctx context.Context
	tx  pgx.Tx
	m   map[string]recordedDeploy
	err error
}

type recordedDeploy struct {
	commit string
	ref    map[string]any
	at     time.Time
	// found: a deploy_seen event names the environment; ok: it identifies
	// what runs (a commit and a reference object that is not empty);
	// invalid: why a record found does not (review 427 A1, B1).
	found   bool
	ok      bool
	invalid string
}

func newRecordedDeploys(ctx context.Context, tx pgx.Tx) *recordedDeploys {
	return &recordedDeploys{ctx: ctx, tx: tx, m: map[string]recordedDeploy{}}
}

// read is a Deployed: what env runs, as recorded; ok is false when nothing
// is recorded for it.
func (r *recordedDeploys) read(_ context.Context, env string) (string, map[string]any, bool) {
	d := r.get(env)
	return d.commit, d.ref, d.ok
}

func (r *recordedDeploys) get(env string) recordedDeploy {
	if d, ok := r.m[env]; ok {
		return d
	}
	var d recordedDeploy
	if r.err != nil {
		return d
	}
	var refDoc []byte
	err := r.tx.QueryRow(r.ctx, `SELECT COALESCE(body_json->>'commit', ''), (body_json->'deploy_ref')::text, at FROM agentrt.event
	  WHERE event_type = 'deploy_seen' AND body_json @> jsonb_build_object('env', $1::text) ORDER BY event_id DESC LIMIT 1`,
		env).Scan(&d.commit, &refDoc, &d.at)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		r.err = err
		return d
	default:
		// The reference as SeeDeploy recorded it. Only an object that names
		// something identifies a deployment: a null reference would encode as
		// {} and match a pass recorded against {} (review 427 A1).
		d.found = true
		v, perr := canon.Parse(refDoc)
		ref, isObject := v.(map[string]any)
		switch {
		case perr != nil || refDoc == nil:
			d.invalid = "it has no reference"
		case v == nil:
			d.invalid = "its reference is null"
		case !isObject:
			d.invalid = "its reference is not an object"
		case len(ref) == 0:
			d.invalid = "its reference is empty"
		case strings.TrimSpace(d.commit) == "":
			d.invalid = "it names no commit"
		default:
			d.ok, d.ref = true, ref
		}
	}
	r.m[env] = d
	return d
}

// explained remembers what each atom read during one evaluation, in the
// order the rule read them, for the reason. The rule reads each gate once
// (plan.Rule.Eval), so each is remembered once.
type explained struct {
	plan.Facts
	atoms []atomReading
	// gates is the projection's readings of gates, by environment, test and
	// spec: the deployment they are read against is the projection's.
	gates map[string]plan.Tri
	// status is what became of a gate that has not passed (gateStatus); nil
	// for none.
	status func(env, test, spec string) string
}

type atomReading struct {
	what, env string // env is a gate's environment; what is its test id
	value     plan.Tri
}

func (e *explained) saw(what, env string, ok bool) bool {
	v := plan.False
	if ok {
		v = plan.True
	}
	e.atoms = append(e.atoms, atomReading{what: what, env: env, value: v})
	return ok
}

func (e *explained) TaskAccepted(id string) bool {
	return e.saw("task "+id+" accepted", "", e.Facts.TaskAccepted(id))
}

func (e *explained) ApprovalExecuted(n int64) bool {
	return e.saw(fmt.Sprintf("approval #%d executed", n), "", e.Facts.ApprovalExecuted(n))
}

func (e *explained) QuestionAnswered(q, option string) bool {
	return e.saw("question "+q+" answered "+option, "", e.Facts.QuestionAnswered(q, option))
}

func (e *explained) CISucceeded(repo, commit string) bool {
	return e.saw("CI of "+repo+"@"+short(commit)+" succeeded", "", e.Facts.CISucceeded(repo, commit))
}

func (e *explained) OwnerSignoff(ref string) bool {
	return e.saw("the Owner signed off "+ref, "", e.Facts.OwnerSignoff(ref))
}

func (e *explained) ManualRecorded(note string) bool {
	return e.saw("recorded by hand: "+note, "", e.Facts.ManualRecorded(note))
}

func (e *explained) GatePassed(env, test, specSHA256 string) plan.Tri {
	key := env + "\x00" + test + "\x00" + specSHA256
	v, read := e.gates[key]
	if !read {
		v = e.Facts.GatePassed(env, test, specSHA256)
		if e.gates != nil {
			e.gates[key] = v
		}
	}
	what := test
	if v == plan.False && e.status != nil {
		if s := e.status(env, test, specSHA256); s != "" {
			what = test + " (" + s + ")"
		}
	}
	e.atoms = append(e.atoms, atomReading{what: what, env: env, value: v})
	return v
}

// reason names the atoms that read what the rule came to: the gates by
// environment, with the deployment they were checked against, then the
// other atoms.
func (e *explained) reason(value plan.Tri, deploys *recordedDeploys) string {
	var parts []string
	var envs []string
	tests := map[string][]string{}
	for _, a := range e.atoms {
		if a.value != value {
			continue
		}
		if a.env == "" {
			if value == plan.False {
				parts = append(parts, "not yet: "+a.what)
			} else {
				parts = append(parts, a.what)
			}
			continue
		}
		if _, ok := tests[a.env]; !ok {
			envs = append(envs, a.env)
		}
		tests[a.env] = append(tests[a.env], a.what)
	}
	var gates []string
	for _, env := range envs {
		list := strings.Join(tests[env], ", ")
		d := deploys.get(env)
		at := fmt.Sprintf("%s at %s, the deployment recorded %s", env, short(d.commit), d.at.UTC().Format(time.RFC3339))
		switch {
		case value == plan.Unknown && !d.found:
			gates = append(gates, list+": what "+env+" runs is not recorded yet")
		case value == plan.Unknown && !d.ok:
			gates = append(gates, fmt.Sprintf("%s: the deployment recorded for %s at %s does not identify what runs: %s", list, env, d.at.UTC().Format(time.RFC3339), d.invalid))
		case value == plan.Unknown:
			gates = append(gates, list+": the deployment recorded for "+env+" cannot be read")
		case value == plan.True:
			gates = append(gates, list+" passed on "+at)
		default:
			gates = append(gates, list+": no pass on "+at)
		}
	}
	return strings.Join(append(gates, parts...), "; ")
}

// short is a commit as people read it.
func short(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
