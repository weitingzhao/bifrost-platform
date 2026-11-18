// Package regressions keeps the known regressions on main (2026-11-17, the
// coordinator: information, no gate). A line that finds main broken records
// it (main.regression.known); the line that fixes it records the fix
// (main.regression.fixed). Both are keyed <repo>@<introduced commit>, the
// event's subject: a known and its fixed pair on it. A PROD release's
// context lists those still open (server.openRegressions reads List), and
// the seat starting a PROD release is woken with them.
//
// The ledger is the truth: nothing is kept elsewhere, and the open ones are
// computed from it whenever they are asked for.
package regressions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The two topics (registered in bus/registry/rocket.yaml).
const (
	TypeKnown = "main.regression.known"
	TypeFixed = "main.regression.fixed"
)

// MaxImpact is the longest impact line, in characters.
const MaxImpact = 200

var (
	repoRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	shaRE  = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// ErrNotKnown: a fix was recorded for a key no known regression has.
var ErrNotKnown = errors.New("no known regression on main has this repo and introduced commit")

// Stream is the ledger stream of one repository's regressions.
func Stream(repo string) string { return "regression:" + repo }

// Key is the pairing key and the events' subject: <repo>@<introduced>.
func Key(repo, introduced string) string { return repo + "@" + introduced }

// Ref is the platform read the events point at.
func Ref(repo string) string { return "/api/v1/bus/regressions?repo=" + url.QueryEscape(repo) }

// Regression is one known regression, open or fixed.
type Regression struct {
	Repo       string    `json:"repo"`
	Introduced string    `json:"introduced"`
	Impact     string    `json:"impact"`
	By         string    `json:"by"`
	KnownAt    time.Time `json:"known_at"`
	Fixed      string    `json:"fixed,omitempty"`
	FixedBy    string    `json:"fixed_by,omitempty"`
	FixedAt    time.Time `json:"fixed_at,omitzero"`
}

// Check says what is wrong with a report, before anything is written.
func Check(repo, introduced, impact string) error {
	switch {
	case !repoRE.MatchString(repo):
		return fmt.Errorf("repo %q is not a repository name", repo)
	case !shaRE.MatchString(introduced):
		return fmt.Errorf("introduced %q is not a 40-character commit id", introduced)
	case strings.TrimSpace(impact) == "":
		return errors.New("impact says in a line what is broken")
	case len([]rune(impact)) > MaxImpact:
		return fmt.Errorf("impact is over %d characters", MaxImpact)
	}
	return nil
}

func body(typ, repo, introduced string) map[string]any {
	change := "known"
	if typ == TypeFixed {
		change = "fixed"
	}
	k := Key(repo, introduced)
	return map[string]any{"repo": repo, "introduced": introduced, "subject": k, "key": k, "ref": Ref(repo), "change": change}
}

// Known records a known regression as w (a known reported again for the
// same key is recorded again; the newest one counts).
func Known(ctx context.Context, db eventlog.DB, w actor.Writer, repo, introduced, impact string) (int64, error) {
	if err := Check(repo, introduced, impact); err != nil {
		return 0, err
	}
	b := body(TypeKnown, repo, introduced)
	b["impact"] = strings.TrimSpace(impact)
	b["by"] = w.Actor
	return write(ctx, db, w, repo, TypeKnown, b, "")
}

// Fixed records the fix of a known regression as w; ErrNotKnown when no
// known regression has this key.
func Fixed(ctx context.Context, db eventlog.DB, w actor.Writer, repo, introduced, fixed string) (int64, error) {
	if !repoRE.MatchString(repo) || !shaRE.MatchString(introduced) || !shaRE.MatchString(fixed) {
		return 0, fmt.Errorf("repo, introduced and fixed are a repository name and two 40-character commit ids")
	}
	b := body(TypeFixed, repo, introduced)
	b["fixed"] = fixed
	b["by"] = w.Actor
	return write(ctx, db, w, repo, TypeFixed, b, Key(repo, introduced))
}

func write(ctx context.Context, db eventlog.DB, w actor.Writer, repo, typ string, b map[string]any, mustBeKnown string) (int64, error) {
	var id int64
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := actor.Ensure(ctx, tx, w, false); err != nil {
			return err
		}
		if mustBeKnown != "" {
			ok, err := eventlog.Has(ctx, tx, Stream(repo), TypeKnown, map[string]string{"subject": mustBeKnown})
			if err != nil {
				return err
			}
			if !ok {
				return ErrNotKnown
			}
		}
		a, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: Stream(repo), Type: typ, Actor: w.Actor, Body: b})
		id = a.EventID
		return err
	})
	return id, err
}

// listLimit bounds the events one listing reads (regressions are rare: a
// handful a week).
const listLimit = 2000

type ledgerRow struct {
	Type   string         `json:"event_type"`
	Actor  string         `json:"actor_id"`
	At     time.Time      `json:"at"`
	Body   map[string]any `json:"body_json"`
	ID     int64          `json:"event_id"`
	Stream string         `json:"event_stream_id"`
}

// List is every known regression of the repository (all when repo is ""),
// with its fix when one is recorded, newest first; open keeps only those
// with none.
func List(ctx context.Context, db eventlog.DB, repo string, open bool) ([]Regression, error) {
	var doc []byte
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		doc, _, err = eventlog.LatestEvents(ctx, tx, 0, listLimit, eventlog.EventFilter{Types: []string{TypeKnown, TypeFixed}})
		return err
	})
	if err != nil {
		return nil, err
	}
	var rows []ledgerRow
	if err := json.Unmarshal(doc, &rows); err != nil {
		return nil, err
	}
	return fold(rows, repo, open), nil
}

// fold pairs the events by key (rows newest first): the newest known of a
// key is its report, its newest fixed its fix.
func fold(rows []ledgerRow, repo string, open bool) []Regression {
	s := func(b map[string]any, k string) string { v, _ := b[k].(string); return v }
	by := map[string]*Regression{}
	var keys []string
	for i := len(rows) - 1; i >= 0; i-- { // oldest first, so the newest of each kind wins
		r := rows[i]
		rp, intro := s(r.Body, "repo"), s(r.Body, "introduced")
		if repo != "" && rp != repo {
			continue
		}
		k := Key(rp, intro)
		g, ok := by[k]
		if !ok {
			g = &Regression{Repo: rp, Introduced: intro}
			by[k] = g
			keys = append(keys, k)
		}
		switch r.Type {
		case TypeKnown:
			g.Impact, g.By, g.KnownAt = s(r.Body, "impact"), s(r.Body, "by"), r.At
			// A known reported again after a fix reopens it.
			if !g.FixedAt.IsZero() && r.At.After(g.FixedAt) {
				g.Fixed, g.FixedBy, g.FixedAt = "", "", time.Time{}
			}
		case TypeFixed:
			g.Fixed, g.FixedBy, g.FixedAt = s(r.Body, "fixed"), s(r.Body, "by"), r.At
		}
	}
	out := []Regression{}
	for _, k := range keys {
		g := by[k]
		if g.KnownAt.IsZero() || (open && g.Fixed != "") {
			continue
		}
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].KnownAt.After(out[j].KnownAt) })
	return out
}

func init() {
	fields := []eventlog.Field{
		{Name: "repo", Kind: eventlog.Str},
		{Name: "introduced", Kind: eventlog.Str},
		{Name: "by", Kind: eventlog.Str},
		{Name: "subject", Kind: eventlog.Str},
		{Name: "key", Kind: eventlog.Str},
		{Name: "ref", Kind: eventlog.Str},
		{Name: "change", Kind: eventlog.Str},
	}
	values := map[string]eventlog.Value{
		"repo":       eventlog.Shaped("a repository name", repoRE),
		"introduced": eventlog.Shaped("a 40-character commit id", shaRE),
	}
	check := func(change string) func(map[string]any) error {
		return func(b map[string]any) error {
			s := func(k string) string { v, _ := b[k].(string); return v }
			k := Key(s("repo"), s("introduced"))
			if b["subject"] != k || b["key"] != k || b["ref"] != Ref(s("repo")) || b["change"] != change {
				return eventlog.Invalid("%s: subject and key are <repo>@<introduced>, ref the repository's regressions, change %s", change, change)
			}
			return nil
		}
	}
	knownFields := append(append([]eventlog.Field{}, fields...), eventlog.Field{Name: "impact", Kind: eventlog.Str})
	fixedFields := append(append([]eventlog.Field{}, fields...), eventlog.Field{Name: "fixed", Kind: eventlog.Str})
	fixedValues := map[string]eventlog.Value{"repo": values["repo"], "introduced": values["introduced"], "fixed": eventlog.Shaped("a 40-character commit id", shaRE)}
	eventlog.Declare(eventlog.Vocabulary{
		Bodies: map[string][]eventlog.Field{TypeKnown: knownFields, TypeFixed: fixedFields},
		Values: map[string]map[string]eventlog.Value{TypeKnown: values, TypeFixed: fixedValues},
		After:  map[string]func(map[string]any) error{TypeKnown: check("known"), TypeFixed: check("fixed")},
	})
}
