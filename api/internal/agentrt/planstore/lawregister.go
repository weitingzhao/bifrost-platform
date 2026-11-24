package planstore

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The register of laws (closed loop, Owner 2026-11-23: a ticket for a law
// changes a real record of the law in the system; a ticket on its own means
// nothing). Each law is a stream, law:<n>, of law_recorded events; its last
// event is the law as it stands. Two writers:
//
//   - the transcription of OWNER_LAW.md (the coordinator's table, which until
//     now was the only register): a row not in the register, or one that
//     changed there, is recorded as op "transcribed". The file is a source
//     only while no approved card has changed that law.
//   - an approved law_amend card (the approvals domain's): add records the next
//     number, amend a new version of the law's text, repeal its status. Once a
//     card changed a law, the card is the law and the file no longer moves it.
//
// The approvals domain reads CurrentLaw and NextLawNumber when a law_amend
// card is created: an amend or a repeal must name a law in the register and
// quote its text as it stands.

// LawRow is one row of OWNER_LAW.md's register table.
type LawRow struct {
	Law, SaidOn, Text, Where, EnforcedBy, Status, OldWording string
}

// Law is a law as the register holds it now.
type Law struct {
	Law        string    `json:"law"`
	Version    int       `json:"version"`
	Op         string    `json:"op"` // transcribed, add, amend or repeal
	SaidOn     string    `json:"said_on,omitempty"`
	Text       string    `json:"text"`
	Where      string    `json:"where,omitempty"`
	EnforcedBy string    `json:"enforced_by,omitempty"`
	Status     string    `json:"status"`
	OldWording string    `json:"old_wording,omitempty"`
	Source     string    `json:"source"`
	ApprovalID string    `json:"approval_id,omitempty"`
	Standards  any       `json:"standards,omitempty"`
	At         time.Time `json:"at"`
}

// LawStream is a law's stream.
func LawStream(n string) string { return "law:" + n }

// RepealedStatus is the status of a law a card repealed.
const RepealedStatus = "废止"

var lawRow = regexp.MustCompile(`^\|\s*(\d+)\s*\|`)

// ParseLawTable reads the rows of OWNER_LAW.md's register table (the section
// 「## 登记」): seven cells each, a pipe inside backticks being text.
func ParseLawTable(md string) ([]LawRow, error) {
	i := strings.Index(md, "## 登记")
	if i < 0 {
		return nil, errors.New("OWNER_LAW.md: no 「## 登记」 section")
	}
	var out []LawRow
	seen := map[string]bool{}
	for _, line := range strings.Split(md[i:], "\n") {
		if !lawRow.MatchString(line) {
			continue
		}
		cells := splitRow(line)
		if len(cells) != 7 {
			return nil, fmt.Errorf("OWNER_LAW.md: row %q has %d cells, not 7", cells[0], len(cells))
		}
		if seen[cells[0]] {
			return nil, fmt.Errorf("OWNER_LAW.md: law %s appears twice", cells[0])
		}
		seen[cells[0]] = true
		out = append(out, LawRow{Law: cells[0], SaidOn: cells[1], Text: cells[2], Where: cells[3], EnforcedBy: cells[4], Status: cells[5], OldWording: cells[6]})
	}
	if len(out) == 0 {
		return nil, errors.New("OWNER_LAW.md: the register table has no row")
	}
	return out, nil
}

func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	var cells []string
	var cur strings.Builder
	tick := false
	for _, r := range s {
		switch {
		case r == '`':
			tick = !tick
			cur.WriteRune(r)
		case r == '|' && !tick:
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

// lockRegister serializes the register's writes: numbering and the
// transcription read and write it under this lock.
func lockRegister(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(742111, 0)`)
	return err
}

func lawOf(r eventlog.Row) Law {
	s := func(k string) string { v, _ := r.Body[k].(string); return v }
	v, _ := r.Body["version"].(float64)
	return Law{Law: s("law"), Version: int(v), Op: s("op"), SaidOn: s("said_on"), Text: s("text"), Where: s("where"),
		EnforcedBy: s("enforced_by"), Status: s("status"), OldWording: s("old_wording"), Source: s("source"),
		ApprovalID: s("approval_id"), Standards: r.Body["standards"], At: r.At}
}

// CurrentLaws is every law in the register as it stands, by number.
func CurrentLaws(ctx context.Context, db eventlog.DB) ([]Law, error) {
	var out []Law
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		out, err = currentLaws(ctx, tx)
		return err
	})
	return out, err
}

func currentLaws(ctx context.Context, tx pgx.Tx) ([]Law, error) {
	rows, err := eventlog.LatestOfStreams(ctx, tx, "law_recorded", "law:", time.Time{})
	if err != nil {
		return nil, err
	}
	out := make([]Law, 0, len(rows))
	for _, r := range rows {
		out = append(out, lawOf(r))
	}
	sort.Slice(out, func(i, j int) bool { return lawNumber(out[i].Law) < lawNumber(out[j].Law) })
	return out, nil
}

func lawNumber(s string) int { n, _ := strconv.Atoi(s); return n }

// CurrentLaw is one law as it stands; false when the register has no such law.
func CurrentLaw(ctx context.Context, db eventlog.DB, n string) (Law, bool, error) {
	var law Law
	var ok bool
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		r, found, err := eventlog.Last(ctx, tx, LawStream(n))
		if err != nil || !found || r.Type != "law_recorded" {
			return err
		}
		law, ok = lawOf(r), true
		return nil
	})
	return law, ok, err
}

// NextLawNumber is the number an added law gets: one past the largest. It is
// "" while the register is empty (nothing transcribed yet), when no number
// can be given safely.
func NextLawNumber(ctx context.Context, db eventlog.DB) (string, error) {
	var next string
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var err error
		next, err = nextLawNumber(ctx, tx)
		return err
	})
	return next, err
}

func nextLawNumber(ctx context.Context, tx pgx.Tx) (string, error) {
	laws, err := currentLaws(ctx, tx)
	if err != nil || len(laws) == 0 {
		return "", err
	}
	return strconv.Itoa(lawNumber(laws[len(laws)-1].Law) + 1), nil
}

func appendLaw(ctx context.Context, tx pgx.Tx, prev Law, have bool, l Law) error {
	l.Version = 1
	if have {
		l.Version = prev.Version + 1
	}
	body := map[string]any{"law": l.Law, "version": l.Version, "op": l.Op, "text": l.Text, "status": l.Status, "source": l.Source}
	for k, v := range map[string]string{"said_on": l.SaidOn, "where": l.Where, "enforced_by": l.EnforcedBy, "old_wording": l.OldWording, "approval_id": l.ApprovalID} {
		if v != "" {
			body[k] = v
		}
	}
	if l.Standards != nil {
		body["standards"] = l.Standards
	}
	_, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: LawStream(l.Law), Type: "law_recorded", Actor: "controller", Body: body})
	return err
}

func lastLaw(ctx context.Context, tx pgx.Tx, n string) (Law, bool, error) {
	r, found, err := eventlog.Last(ctx, tx, LawStream(n))
	if err != nil || !found {
		return Law{}, false, err
	}
	return lawOf(r), true, nil
}

// TranscribeLaws records the table's rows the register does not hold as they
// read, and returns how many it recorded. source names the file and commit.
// A law an approved card changed is left as the card made it.
func TranscribeLaws(ctx context.Context, db eventlog.DB, rows []LawRow, source string) (int, error) {
	n := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockRegister(ctx, tx); err != nil {
			return err
		}
		for _, row := range rows {
			prev, have, err := lastLaw(ctx, tx, row.Law)
			if err != nil {
				return err
			}
			if have && prev.Op != "transcribed" {
				continue
			}
			if have && prev.SaidOn == row.SaidOn && prev.Text == row.Text && prev.Where == row.Where && prev.EnforcedBy == row.EnforcedBy &&
				prev.Status == row.Status && prev.OldWording == row.OldWording {
				continue
			}
			if err := appendLaw(ctx, tx, prev, have, Law{Law: row.Law, Op: "transcribed", SaidOn: row.SaidOn, Text: row.Text, Where: row.Where,
				EnforcedBy: row.EnforcedBy, Status: row.Status, OldWording: row.OldWording, Source: source}); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	return n, err
}

// registerCard writes an approved card into the register, once (by its
// approval id), and returns the law's number. A card waits while the
// register is empty: an add's number would be wrong, an amend's law missing.
func registerCard(ctx context.Context, tx pgx.Tx, c LawCard) (string, bool, error) {
	if err := lockRegister(ctx, tx); err != nil {
		return "", false, err
	}
	if done, err := eventlog.HasBody(ctx, tx, "law_recorded", "", map[string]any{"approval_id": c.ApprovalID}); err != nil || done {
		return "", false, err
	}
	// Nothing transcribed yet: no number is safe and no law is there to
	// amend; the card waits for a later pass.
	next, err := nextLawNumber(ctx, tx)
	if err != nil || next == "" {
		return "", false, err
	}
	n := c.Law
	if c.Op == "add" {
		n = next
	}
	prev, have, err := lastLaw(ctx, tx, n)
	if err != nil {
		return "", false, err
	}
	if c.Op != "add" && !have {
		return "", false, eventlog.Invalid("law_amend %s: the register has no law %s", c.ApprovalID, n)
	}
	var standards []any
	for _, s := range c.Standards {
		standards = append(standards, map[string]any{"id": s.ID, "standard": s.Standard, "measure": s.Measure})
	}
	l := Law{Law: n, Op: c.Op, Text: c.After, Status: "已批（law_amend " + c.ApprovalID + "）", Source: "law_amend " + c.ApprovalID, ApprovalID: c.ApprovalID,
		SaidOn: prev.SaidOn, Where: prev.Where, EnforcedBy: prev.EnforcedBy, OldWording: prev.OldWording}
	if standards != nil {
		l.Standards = standards
	}
	if c.Op == "repeal" {
		l.Text, l.Status = prev.Text, RepealedStatus
	}
	if c.Op == "add" {
		l.SaidOn = c.DecidedOn
	}
	return n, true, appendLaw(ctx, tx, prev, have, l)
}
