package planstore

import (
	"context"
	"encoding/json"
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
	Code       string    `json:"code,omitempty"` // LAW-k (lawcode.go); empty for material
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
	Frame      *LawFrame `json:"frame,omitempty"`
	At         time.Time `json:"at"`
}

// LawStream is a law's stream.
func LawStream(n string) string { return "law:" + n }

// RepealedStatus is the status of a law a card repealed.
const RepealedStatus = "废止"

var lawRow = regexp.MustCompile(`^\|\s*(\d+|(?:DAO|CONSTITUTION|LAW|BYLAW)-\d+)\s*\|`)

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
		cells[0] = LawNumberOf(cells[0])
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
	code := s("code")
	if code == "" {
		code = LawCode(s("law"))
	}
	return Law{Law: s("law"), Code: code, Version: int(v), Op: s("op"), SaidOn: s("said_on"), Text: s("text"), Where: s("where"),
		EnforcedBy: s("enforced_by"), Status: s("status"), OldWording: s("old_wording"), Source: s("source"),
		ApprovalID: s("approval_id"), Standards: r.Body["standards"], Frame: frameOf(r.Body["frame"]), At: r.At}
}

// frameBody is a frame as an event body holds it: integers as int, lists as
// []any, only the fields set (the ledger's canonical form).
func frameBody(f *LawFrame) map[string]any {
	m := map[string]any{}
	if f.Level != nil {
		m["level"] = *f.Level
	}
	for k, v := range map[string]string{"scope": f.Scope, "subject": f.Subject, "modality": f.Modality, "condition": f.Condition,
		"parent": f.Parent, "distinct_from": f.DistinctFrom, "executor": f.Executor, "enforcement": f.Enforcement,
		"evidence": f.Evidence, "expires_on": f.ExpiresOn, "enforce": f.Enforce} {
		if v != "" {
			m[k] = v
		}
	}
	for k, v := range map[string][]string{"depends_on": f.DependsOn, "supersedes": f.Supersedes, "absorbs": f.Absorbs} {
		if len(v) > 0 {
			l := make([]any, len(v))
			for i, x := range v {
				l[i] = x
			}
			m[k] = l
		}
	}
	if r := f.Review; r != nil {
		rv := map[string]any{"by": r.By, "at": r.At, "verdict": r.Verdict}
		if r.Note != "" {
			rv["note"] = r.Note
		}
		m["review"] = rv
	}
	return m
}

// frameOf reads a recorded frame back.
func frameOf(raw any) *LawFrame {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var f LawFrame
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	return &f
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
	if err != nil {
		return "", err
	}
	// A purged law's number is never given again (lawpurge.go).
	purged, _, err := purgedLaws(ctx, tx)
	if err != nil || len(laws)+len(purged) == 0 {
		return "", err
	}
	top := 0
	if len(laws) > 0 {
		top = lawNumber(laws[len(laws)-1].Law)
	}
	for _, n := range purged {
		top = max(top, n)
	}
	return strconv.Itoa(top + 1), nil
}

func appendLaw(ctx context.Context, tx pgx.Tx, prev Law, have bool, l Law) error {
	l.Version = 1
	if have {
		l.Version = prev.Version + 1
	}
	body := map[string]any{"law": l.Law, "version": l.Version, "op": l.Op, "text": l.Text, "status": l.Status, "source": l.Source}
	for k, v := range map[string]string{"said_on": l.SaidOn, "where": l.Where, "enforced_by": l.EnforcedBy, "old_wording": l.OldWording, "approval_id": l.ApprovalID, "code": l.Code} {
		if v != "" {
			body[k] = v
		}
	}
	if l.Standards != nil {
		body["standards"] = l.Standards
	}
	if l.Frame != nil {
		body["frame"] = frameBody(l.Frame)
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
// A law an approved card changed is left as the card made it. A row whose
// number the register lacks is taken only while the register is empty (its
// seed): from 2026-11-24 a new law comes only by a law_amend card, and
// OWNER_LAW.md is rendered from the register (infra
// scripts/laws/render_owner_law.py), not written by hand.
func TranscribeLaws(ctx context.Context, db eventlog.DB, rows []LawRow, source string) (int, error) {
	n := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		if err := lockRegister(ctx, tx); err != nil {
			return err
		}
		next, err := nextLawNumber(ctx, tx)
		if err != nil {
			return err
		}
		seeding := next == ""
		for _, row := range rows {
			prev, have, err := lastLaw(ctx, tx, row.Law)
			if err != nil {
				return err
			}
			if have && prev.Op != "transcribed" {
				continue
			}
			// From 2026-11-24 a law is added only by an approved law_amend
			// card: a number the register lacks is transcribed only into an
			// empty register (its seed), never written in by hand later.
			if !have && !seeding {
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
	// A card whose law was purged stays out (lawpurge.go: the tombstone
	// keeps its approval id).
	if _, purged, err := purgedLaws(ctx, tx); err != nil || purged[c.ApprovalID] {
		return "", false, err
	}
	if c.Op == "purge" {
		return "", true, purgeByCard(ctx, tx, c)
	}
	// Nothing transcribed yet: no number is safe and no law is there to
	// amend; the card waits for a later pass.
	next, err := nextLawNumber(ctx, tx)
	if err != nil || next == "" {
		return "", false, err
	}
	if c.Op == "repeal_many" {
		for _, rp := range c.Repeals {
			prev, have, err := lastLaw(ctx, tx, rp.Law)
			if err != nil {
				return "", false, err
			}
			if !have {
				return "", false, eventlog.Invalid("law_amend %s: the register has no law %s", c.ApprovalID, rp.Law)
			}
			l := prev
			l.Op, l.Status, l.Source, l.ApprovalID, l.Standards = "repeal", RepealedStatus, "law_amend "+c.ApprovalID+"："+rp.Reason, c.ApprovalID, nil
			if err := appendLaw(ctx, tx, prev, true, l); err != nil {
				return "", false, err
			}
		}
		return "", true, nil
	}
	if c.Op == "archive" {
		return "", true, archiveMaterial(ctx, tx, c)
	}
	if c.Op == "amend_many" {
		for _, a := range c.Amends {
			a.ApprovalID, a.Number, a.DecidedOn = c.ApprovalID, c.Number, c.DecidedOn
			if _, have, err := lastLaw(ctx, tx, a.Law); err != nil {
				return "", false, err
			} else if !have {
				return "", false, eventlog.Invalid("law_amend %s: the register has no law %s", c.ApprovalID, a.Law)
			}
			if _, err := registerOne(ctx, tx, a, a.Law); err != nil {
				return "", false, err
			}
		}
		return "", true, nil
	}
	if c.Op == "add_many" {
		for _, a := range c.Adds {
			a.ApprovalID, a.Number, a.DecidedOn = c.ApprovalID, c.Number, c.DecidedOn
			n, err := nextLawNumber(ctx, tx)
			if err != nil {
				return "", false, err
			}
			if _, err := registerOne(ctx, tx, a, n); err != nil {
				return "", false, err
			}
		}
		return "", true, nil
	}
	n := c.Law
	if c.Op == "add" {
		n = next
	}
	num, err := registerOne(ctx, tx, c, n)
	return num, err == nil, err
}

// archiveMaterial repeals the material an archive card names that a law in
// force still absorbs; the reason names the laws that absorb it.
func archiveMaterial(ctx context.Context, tx pgx.Tx, c LawCard) error {
	laws, err := currentLaws(ctx, tx)
	if err != nil {
		return err
	}
	now := time.Now()
	absorbed := AbsorbedBy(laws, now)
	for _, rp := range c.Repeals {
		prev, have, err := lastLaw(ctx, tx, rp.Law)
		if err != nil {
			return err
		}
		if !have || LawState(prev, now) != StateMaterial || len(absorbed[rp.Law]) == 0 {
			continue
		}
		l := prev
		l.Op, l.Status, l.ApprovalID, l.Standards = "repeal", RepealedStatus, c.ApprovalID, nil
		l.Source = "归档：被第 " + strings.Join(absorbed[rp.Law], "、") + " 条吸收（law_amend " + c.ApprovalID + "：" + rp.Reason + "）"
		if err := appendLaw(ctx, tx, prev, true, l); err != nil {
			return err
		}
	}
	return nil
}

// registerOne writes one add, amend or repeal of law n.
func registerOne(ctx context.Context, tx pgx.Tx, c LawCard, n string) (string, error) {
	prev, have, err := lastLaw(ctx, tx, n)
	if err != nil {
		return "", err
	}
	if c.Op != "add" && !have {
		return "", eventlog.Invalid("law_amend %s: the register has no law %s", c.ApprovalID, n)
	}
	var standards []any
	for _, s := range c.Standards {
		standards = append(standards, map[string]any{"id": s.ID, "standard": s.Standard, "measure": s.Measure})
	}
	l := Law{Law: n, Op: c.Op, Text: c.After, Status: "已批（law_amend " + c.ApprovalID + "）", Source: "law_amend " + c.ApprovalID, ApprovalID: c.ApprovalID,
		SaidOn: prev.SaidOn, Where: prev.Where, EnforcedBy: prev.EnforcedBy, OldWording: prev.OldWording, Code: prev.Code}
	if c.Op == "add" {
		codes, err := allCodes(ctx, tx)
		if err != nil {
			return "", err
		}
		var level *int
		if c.Frame != nil {
			level = c.Frame.Level
		}
		l.Code = nextCode(codes, level)
	} else if err := levelKept(prev, c.Frame); err != nil {
		return "", err
	}
	if c.Op != "repeal" {
		l.Frame = c.Frame
	}
	if standards != nil {
		l.Standards = standards
	}
	if c.Op == "repeal" {
		l.Text, l.Status = prev.Text, RepealedStatus
	}
	if c.Op == "add" {
		l.SaidOn = c.DecidedOn
	}
	if err := appendLaw(ctx, tx, prev, have, l); err != nil {
		return "", err
	}
	// What it supersedes is repealed with it (framework: 取代).
	if l.Frame != nil {
		for _, sn := range l.Frame.Supersedes {
			old, ok, err := lastLaw(ctx, tx, sn)
			if err != nil {
				return "", err
			}
			if !ok || old.Status == RepealedStatus {
				continue
			}
			r := old
			r.Op, r.Status, r.Source, r.ApprovalID, r.Standards = "repeal", RepealedStatus, "被第 "+n+" 条取代（law_amend "+c.ApprovalID+"）", c.ApprovalID, nil
			if err := appendLaw(ctx, tx, old, true, r); err != nil {
				return "", err
			}
		}
	}
	return n, nil
}

// allCodes are the codes given so far: the register's laws and the purged
// laws' tombstones, so no code is given twice.
func allCodes(ctx context.Context, tx pgx.Tx) ([]string, error) {
	laws, err := currentLaws(ctx, tx)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range laws {
		if l.Code != "" {
			out = append(out, l.Code)
		}
	}
	tomb, err := eventlog.LatestOfStreams(ctx, tx, "law_purged", "law-purged:", time.Time{})
	if err != nil {
		return nil, err
	}
	for _, r := range tomb {
		if c, _ := r.Body["code"].(string); c != "" {
			out = append(out, c)
		} else if n, _ := r.Body["law"].(string); LawCode(n) != "" {
			out = append(out, LawCode(n))
		}
	}
	return out, nil
}

// levelKept refuses an amend that moves a coded law to another level: the
// code says the level, so a law that changes level is repealed and added
// again with a new code (Owner 2026-11-25).
func levelKept(prev Law, f *LawFrame) error {
	if prev.Code == "" || prev.Frame == nil || prev.Frame.Level == nil || f == nil || f.Level == nil {
		return nil
	}
	if *prev.Frame.Level != *f.Level {
		return eventlog.Invalid("law_amend: %s is at level %d; a law that changes level is repealed and added again with a new code, not amended", prev.Code, *prev.Frame.Level)
	}
	return nil
}
