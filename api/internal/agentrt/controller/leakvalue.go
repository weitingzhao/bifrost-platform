package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// The leak value pass (store/leakvalue.go): a place that keeps leaking
// errors to STG or PROD is marked for a check to be added back, and once a
// week the pass proposes one goal for it, unless the goal of an earlier
// week is still open. The goal's key names the place and the week, so a
// retry gets the same goal; a leak_value note on the controller's stream
// records it, and the mark carries its mission.

// LeakValueEvery is how often the pass reads the marks.
const LeakValueEvery = time.Hour

// LeakValueNeedsOwner is why a leak value goal's cards wait for the Owner.
const LeakValueNeedsOwner = "adding a check back changes every task's cost"

// leakValueLabelMax is the most of a place's label a goal's title quotes.
const leakValueLabelMax = 80

// leakValueRead is the projection the pass reads (a test replaces it).
var leakValueRead = store.LeakValue

// startLeakValue runs the pass hourly when the controller can read main,
// which a goal needs.
func (c *Controller) startLeakValue(ctx context.Context, wg *sync.WaitGroup) {
	if c.leakValueMain() == nil {
		return
	}
	c.every(ctx, wg, "agentrt-leak-value", LeakValueEvery, func() error {
		passCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
		defer cancel()
		_, err := c.LeakValuePass(passCtx)
		return err
	})
}

// leakValueMain reads bifrost-platform's main: the landing's reader, else
// the full CI's; nil for none.
func (c *Controller) leakValueMain() func(ctx context.Context, repo string) (string, error) {
	switch {
	case c.Drill != nil && c.Drill.Main != nil:
		return c.Drill.Main
	case c.FullCI != nil && c.FullCI.Main != nil:
		main := c.FullCI.Main
		return func(ctx context.Context, _ string) (string, error) { return main(ctx) }
	}
	return nil
}

// LeakValuePass is one pass: for each marked place not noted this week and
// with no open goal of an earlier note, a goal and its note. It returns how
// many goals it proposed.
func (c *Controller) LeakValuePass(ctx context.Context) (int, error) {
	main := c.leakValueMain()
	if main == nil {
		return 0, nil
	}
	now := c.clock()
	doc, err := leakValueRead(ctx, c.DB, now, store.LeakValueWeeksDefault)
	if err != nil {
		return 0, fmt.Errorf("leak value: %w", err)
	}
	week := store.LeakValueWeek(now)
	proposed := 0
	var errs []error
	for _, row := range doc.Rows {
		if row.Mark == nil || row.Place == store.LeakValueUnattributed {
			continue
		}
		did, err := c.leakValueAddBack(ctx, main, row, week)
		if err != nil {
			errs = append(errs, fmt.Errorf("leak value %s: %w", row.Place, err))
		}
		if did {
			proposed++
		}
	}
	return proposed, errors.Join(errs...)
}

func (c *Controller) leakValueAddBack(ctx context.Context, main func(context.Context, string) (string, error), row store.LeakValueRow, week string) (bool, error) {
	noted, open, err := store.LeakValueNoted(ctx, c.DB, row.Place, week)
	if err != nil || noted || open != "" {
		return false, err
	}
	label := []rune(row.Label)
	if len(label) > leakValueLabelMax {
		label = append(label[:leakValueLabelMax-1], '…')
	}
	title := "Add a check back for " + string(label)
	out, err := store.ProposeGoal(ctx, c.DB, store.Writer{Actor: "controller"},
		"leakvalue:addback:"+store.LeakValuePlaceDigest(row.Place)+":"+week,
		store.Goal{Title: title, Text: leakValueGoalText(row), Repos: []string{"bifrost-platform"}, NeedsOwner: LeakValueNeedsOwner, Main: main})
	if err != nil {
		return false, err
	}
	mission, _ := out["mission_id"].(string)
	return true, store.NoteLeakValue(ctx, c.DB, fmt.Sprintf("%s keeps leaking errors to STG or PROD since %s; goal %s proposed: %s.",
		row.Label, row.Mark.Since, mission, title), store.LeakValueNote{Place: row.Place, Week: week, Mission: mission})
}

// leakValueGoalText is a marked place's goal: the place, its leaks with
// their records, and what to do.
func leakValueGoalText(row store.LeakValueRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s (%s) let errors through to STG or PROD in more than one of the last four full weeks, since %s.\n\n", row.Label, row.Place, row.Mark.Since)
	b.WriteString("The leaks, with their records:\n")
	for _, l := range row.Leaks {
		fmt.Fprintf(&b, "- %s, %s: %s\n", l.Week, strings.ToUpper(l.Env), l.Record)
	}
	b.WriteString("\nFind the check that would have caught these before they left the place (a test, a verify step or a review rule that was " +
		"dropped or never written), and add it back for this place's tasks. Say in the plan what it costs each task and why the leaks are worth it.\n")
	return b.String()
}
