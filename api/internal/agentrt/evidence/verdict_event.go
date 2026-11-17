package evidence

import (
	"context"
	"encoding/json"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The bus's announcement that a key of the ledger changed its verdict
// (bus/registry/rocket.yaml registers ci.verdict.changed with gate-db as its
// publisher; migration 0026 lets the database take it). RecordEvidence
// writes it in the transaction that wrote the row, after the row, and only
// when EvidenceVerdict of the key's rows is not what it was before the row.
// It is a hint (bus RULES.md E2): the ledger stays the truth, and nothing in
// this package or the gates reads the event.
const VerdictChangedType = "ci.verdict.changed"

// The kinds of key the event names: a CI pipeline's run (RunEvidence, the
// rows ledgerCI reads) or a unit task's (UnitEvidenceRow).
const (
	VerdictKindCI   = "ci"
	VerdictKindUnit = "unit"
)

// verdictActor writes the event: the evidence package already writes its
// notes as the controller (fullci.go), which the platform seeds.
const verdictActor = "controller"

// VerdictRef is where a reader of the event reads the state (the register's
// ref with its placeholders filled).
func VerdictRef(repo, commit string) string {
	return "/api/v1/units/plan?repo=" + repo + "&commit=" + commit
}

// VerdictSubject is "<tree>@<commit>:<kind>", the register's subject; a CI
// key adds its scope, "<tree>@<commit>:ci/full" or ":ci/incremental". The
// envelope carries no data, so a subscriber that cares about full against
// incremental filters on the subject (CI line 2026-11-16: one kind, the
// scope beside it). An incremental key is not the commit's verdict on its
// own: it counts only together with its base's, so a subscriber that gets
// ci/incremental reads the commit-level route back instead of taking it as
// green.
func VerdictSubject(tree, commit, kind, scope string) string {
	if kind == VerdictKindCI && scope != "" {
		kind += "/" + scope
	}
	return tree + "@" + commit + ":" + kind
}

// VerdictStream is the ledger stream of a tree's verdict events: one per
// repository and tree, the register's key (RULES.md §7).
func VerdictStream(repo, tree string) string { return "verdict:" + repo + ":" + tree }

var verdictRepoRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

// VerdictBody is the event's body: the bus's fixed fields (subject, ref, key,
// change; RULES.md §7) and the facts they are made of. unit, task and
// input_key name the ledger key whose verdict changed (one tree has a key
// per task and per what was beside it).
func VerdictBody(repo, commit, tree, kind, scope, unit, task, inputKey, verdict, previous string) map[string]any {
	return map[string]any{
		"repo":      repo,
		"commit":    commit,
		"tree":      tree,
		"kind":      kind,
		"scope":     scope,
		"unit":      unit,
		"task":      task,
		"input_key": inputKey,
		"verdict":   verdict,
		"previous":  previous,
		"subject":   VerdictSubject(tree, commit, kind, scope),
		"ref":       VerdictRef(repo, commit),
		"key":       tree,
		"change":    verdict,
	}
}

func init() {
	verdicts := eventlog.OneOf(EvidenceGreen, EvidenceRed, EvidenceNone)
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{
			VerdictChangedType: {
				{Name: "repo", Kind: eventlog.Str},
				{Name: "commit", Kind: eventlog.ID},
				{Name: "tree", Kind: eventlog.ID},
				{Name: "kind", Kind: eventlog.Str},
				{Name: "scope", Kind: eventlog.Str},
				{Name: "unit", Kind: eventlog.Str},
				{Name: "task", Kind: eventlog.Str},
				{Name: "input_key", Kind: eventlog.ID},
				{Name: "verdict", Kind: eventlog.Str},
				{Name: "previous", Kind: eventlog.Str},
				{Name: "subject", Kind: eventlog.Str},
				{Name: "ref", Kind: eventlog.Str},
				{Name: "key", Kind: eventlog.Str},
				{Name: "change", Kind: eventlog.Str},
			},
		},
		Values: map[string]map[string]eventlog.Value{
			VerdictChangedType: {
				"repo":      eventlog.Shaped("a repository name", verdictRepoRE),
				"commit":    eventlog.Shaped("a 40-character lowercase commit", gitObject),
				"tree":      eventlog.Shaped("a 40-character lowercase tree", gitObject),
				"input_key": eventlog.Shaped("a sha256 in lowercase hex", sha256Hex),
				"kind":      eventlog.OneOf(VerdictKindCI, VerdictKindUnit),
				"verdict":   verdicts,
				"previous":  verdicts,
				"change":    verdicts,
			},
		},
		After: map[string]func(body map[string]any) error{
			VerdictChangedType: func(body map[string]any) error {
				s := func(k string) string { v, _ := body[k].(string); return v }
				switch {
				case body["subject"] != VerdictSubject(s("tree"), s("commit"), s("kind"), s("scope")) || body["ref"] != VerdictRef(s("repo"), s("commit")) || body["key"] != s("tree"):
					return eventlog.Invalid("%s: subject, ref and key are made of tree, commit, kind, scope and repo", VerdictChangedType)
				case body["change"] != s("verdict"):
					return eventlog.Invalid("%s: change is the new verdict", VerdictChangedType)
				case s("verdict") == s("previous"):
					return eventlog.Invalid("%s: the verdict did not change", VerdictChangedType)
				}
				return nil
			},
		},
	})
}

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

// keyVerdict is EvidenceVerdict of one key's rows as tx sees them.
func keyVerdict(ctx context.Context, tx pgx.Tx, e EvidenceRow) (string, error) {
	rows, err := tx.Query(ctx, `SELECT result, scope FROM agentrt.evidence
	  WHERE repo = $1 AND unit = $2 AND task = $3 AND input_key = $4 ORDER BY evidence_id`, e.Repo, e.Unit, e.Task, e.InputKey)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var all []EvidenceRow
	for rows.Next() {
		var r EvidenceRow
		if err = rows.Scan(&r.Result, &r.Scope); err != nil {
			return "", err
		}
		all = append(all, r)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	return EvidenceVerdict(all), nil
}

// verdictTree is the git tree of the row's own repository: the first
// subject of its statement (RunEvidence and UnitEvidenceRow both put it
// first), or "" when the statement does not say.
func verdictTree(e EvidenceRow) string {
	var st struct {
		Subject []struct {
			Digest struct {
				GitTree string `json:"gitTree"`
			} `json:"digest"`
		} `json:"subject"`
	}
	if json.Unmarshal(e.Statement, &st) != nil || len(st.Subject) == 0 || !gitObject.MatchString(st.Subject[0].Digest.GitTree) {
		return ""
	}
	return st.Subject[0].Digest.GitTree
}

// verdictKind is the kind of the row's key (see ciRunRows).
func verdictKind(e EvidenceRow) string {
	var in map[string]json.RawMessage
	if e.Unit == EvidenceUnitRepo && json.Unmarshal(e.Inputs, &in) == nil {
		if _, versioned := in["v"]; !versioned {
			return VerdictKindCI
		}
	}
	return VerdictKindUnit
}

// announceVerdict appends ci.verdict.changed in tx when the key's verdict
// after the row differs from previous, the verdict before it.
func announceVerdict(ctx context.Context, tx pgx.Tx, e EvidenceRow, previous string) error {
	now, err := keyVerdict(ctx, tx, e)
	if err != nil || now == previous {
		return err
	}
	// A ledger with no controller (a database the platform has not seeded:
	// the CI line's own tests write evidence into one) announces nothing;
	// the platform seeds it at migration, so on STG and PROD it is there.
	seeded, err := actor.Exists(ctx, tx, verdictActor)
	if err != nil || !seeded {
		return err
	}
	tree := verdictTree(e)
	_, err = eventlog.Append(ctx, tx, eventlog.Event{Stream: VerdictStream(e.Repo, tree), Type: VerdictChangedType, Actor: verdictActor,
		Body: VerdictBody(e.Repo, e.CommitSHA, tree, verdictKind(e), e.Scope, e.Unit, e.Task, e.InputKey, now, previous)})
	return err
}
