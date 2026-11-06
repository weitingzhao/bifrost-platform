package controller

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The check value pass (the Owner 2026-11-05: every check and retry shows
// its value or goes): once an hour it reads the check value table
// (store/checkvalue.go) and, for each row marked for deletion, proposes one
// goal of deleting it, for the Owner, and records a check_value note on the
// controller's stream. A row gets one goal per week (its key names the week
// marked), and none while the goal of an earlier note of the row is open, as
// the full CI's reds (fullci.go).

// CheckValueEvery is how often the pass runs.
const CheckValueEvery = time.Hour

// checkValueNeedsOwner is why a deletion goal's cards wait for the Owner.
const checkValueNeedsOwner = "deleting a check changes what protects STG and PROD"

// startCheckValue runs the pass when the controller can read main, timed as
// the others are.
func (c *Controller) startCheckValue(ctx context.Context, wg *sync.WaitGroup) {
	if c.DB == nil || c.checkValueMain() == nil {
		return
	}
	c.every(ctx, wg, "agentrt-check-value", CheckValueEvery, func() error {
		passCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		_, err := c.CheckValuePass(passCtx)
		return err
	})
}

// checkValueMain reads a repository's main for a goal: the landing's
// reader, else the full CI's (bifrost-platform's); nil without either.
func (c *Controller) checkValueMain() func(ctx context.Context, repo string) (string, error) {
	if c.Drill != nil && c.Drill.Main != nil {
		return c.Drill.Main
	}
	if f := c.FullCI; f != nil && f.Main != nil {
		return func(ctx context.Context, _ string) (string, error) { return f.Main(ctx) }
	}
	return nil
}

// CheckValuePass is one pass: it returns how many goals it proposed.
func (c *Controller) CheckValuePass(ctx context.Context) (int, error) {
	main := c.checkValueMain()
	if c.DB == nil || main == nil {
		return 0, nil
	}
	doc, err := store.CheckValueAt(ctx, c.DB, store.CheckValueWeeksDefault, c.clock())
	if err != nil {
		return 0, fmt.Errorf("check value: %w", err)
	}
	return c.checkValuePropose(ctx, doc, main)
}

// checkValuePropose proposes the goal of each row doc marks, unless the row
// has its note for the last full week or the goal of an earlier note is
// still open.
func (c *Controller) checkValuePropose(ctx context.Context, doc store.CheckValueDoc, main func(context.Context, string) (string, error)) (int, error) {
	if len(doc.Weeks) < 2 {
		return 0, nil
	}
	week := doc.Weeks[len(doc.Weeks)-2]
	proposed := 0
	var errs []error
	for _, r := range doc.Rows {
		if r.Mark == nil {
			continue
		}
		did, err := c.checkValueRow(ctx, r, week, main)
		if err != nil {
			errs = append(errs, fmt.Errorf("check value %s: %w", r.Key, err))
		}
		if did {
			proposed++
		}
	}
	return proposed, errors.Join(errs...)
}

func (c *Controller) checkValueRow(ctx context.Context, r store.CheckValueRow, week string,
	main func(context.Context, string) (string, error)) (bool, error) {
	if noted, err := store.CheckValueNoted(ctx, c.DB, r.Key, week); err != nil || noted {
		return false, err
	}
	if open, err := store.CheckValueOpenGoal(ctx, c.DB, r.Key); err != nil || open != "" {
		return false, err
	}
	title := "Delete the " + r.Kind + " " + r.Label + "? (check value)"
	out, err := store.ProposeGoal(ctx, c.DB, store.Writer{Actor: "controller"}, "checkvalue:delete:"+r.Key+":"+week, store.Goal{Title: title,
		Text: checkValueGoalText(r), Repos: []string{"bifrost-platform"}, NeedsOwner: checkValueNeedsOwner, Main: main})
	if err != nil {
		return false, err
	}
	mission, _ := out["mission_id"].(string)
	return true, store.NoteCheckValue(ctx, c.DB, fmt.Sprintf("The check value table marked %s for deletion in %s; goal %s proposed: %s.",
		r.Key, week, mission, title), store.CheckValueNote{Key: r.Key, Week: week, Mission: mission})
}

// checkValueGoalText is a marked row's goal: what it is, its numbers in the
// last full weeks, and what to do.
func checkValueGoalText(r store.CheckValueRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The check value table (GET /api/v1/agentrt/check-value) marks the %s %s (%s) for deletion: it ran in each full week since %s, "+
		"and nothing it caught in them was unique (a later check runs every command a caught failure failed on, or another kind of retry "+
		"was used on the same task in the same round).\n\n", r.Kind, r.Label, r.Key, r.Mark.Since)
	b.WriteString("Its last full weeks:\n")
	full := r.Cells
	if len(full) > 0 {
		full = full[:len(full)-1] // the current week is partial
	}
	if len(full) > 4 {
		full = full[len(full)-4:]
	}
	num := func(n *int) string {
		if n == nil {
			return "unknown"
		}
		return strconv.Itoa(*n)
	}
	minutes := func(m *float64) string {
		if m == nil {
			return "unknown"
		}
		return strconv.FormatFloat(*m, 'f', -1, 64)
	}
	for _, cell := range full {
		fmt.Fprintf(&b, "- %s: ran %s, caught %s, unique %s, noise %s, minutes %s, Owner's minutes %s\n", cell.Week, num(cell.Ran), num(cell.Caught),
			num(cell.Unique), num(cell.Noise), minutes(cell.Minutes), minutes(cell.OwnerMinutes))
	}
	b.WriteString("\nDecide with the Owner whether to delete it. If it goes, remove it from the chain and say what protects STG and PROD without it; " +
		"if it stays, record why, so the table stops marking it for that reason.\n")
	return b.String()
}
