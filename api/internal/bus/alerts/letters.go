package alerts

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Letters to the duty seat (2026-11-18, agreed with it): 63 letters in a day,
// nearly all the same few alerts firing and resolving, and it read none of them
// one by one. A repeat now shares its letter: one per alert per 6 hours, one a
// day for an alert that keeps coming back. What must not wait gets a letter
// each time. A new alert is always first in its period, so it always gets one.
const (
	letterWindow        = 6 * time.Hour
	chronicLetterWindow = 24 * time.Hour
	// chronicDays is on how many of the last chronicLookback days an alert
	// must have fired to count as one that keeps coming back.
	chronicDays     = 3
	chronicLookback = 7 * 24 * time.Hour
)

// Rule is what the subscription row sets: the row's id and the namespaces whose
// alerts always get a letter (its params.always_namespaces, comma separated).
type Rule struct {
	Row    string
	Always []string
}

// RuleOf reads a row's id and params.always_namespaces.
func RuleOf(row, always string) Rule {
	r := Rule{Row: row}
	for _, ns := range strings.Split(always, ",") {
		if ns = strings.TrimSpace(ns); ns != "" {
			r.Always = append(r.Always, ns)
		}
	}
	return r
}

// alwaysLetter says the alert gets a letter every time it fires: the row's
// namespaces, critical alerts, node pressure, and Prometheus itself.
func (r Rule) alwaysLetter(a Alert) bool {
	switch {
	case slices.Contains(r.Always, a.Namespace), a.Severity == "critical":
		return true
	case strings.Contains(a.Name, "Pressure"), strings.HasPrefix(a.Name, "Prometheus"), strings.Contains(a.Name, "OOM"):
		return true
	}
	return a.Name == "KubePodCrashLooping" && a.Namespace == "monitoring"
}

// LetterKey is the key of the letter this firing belongs to: letters are written once per key, so firings that share a key share
// a letter. eventID is the event's own id; days is on how many recent days the
// alert fired. "" for an alert that is not firing.
func (r Rule) LetterKey(eventID string, a Alert, at time.Time, days int) string {
	if a.Status != "firing" {
		return ""
	}
	if r.alwaysLetter(a) {
		return "sub:" + r.Row + ":" + eventID
	}
	// An info alert gets one letter a day (2026-11-19). It had none for a few
	// hours after the duty seat first judged CPUThrottlingHigh false; it then
	// found the alert real (gitea held at its CPU limit for about 23.8 hours of
	// 7 days), and a real problem must reach someone. 83 of the week's 243
	// firings were info, so a day per alert keeps them from crowding the inbox.
	window := letterWindow
	if days >= chronicDays || a.Severity == "info" {
		window = chronicLetterWindow
	}
	return fmt.Sprintf("sub:%s:%s:%d", r.Row, Subject(a.Name, a.Namespace), at.Unix()/int64(window/time.Second))
}

// LetterKeyOf reads the event from the ledger and answers LetterKey.
func (r Rule) LetterKeyOf(ctx context.Context, db eventlog.DB, eventID string, n int64, at time.Time) (string, error) {
	key := ""
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		typ, b, err := eventlog.EventBody(ctx, tx, n)
		if err != nil {
			return err
		}
		if typ != Type {
			return fmt.Errorf("alerts: event %d is %s", n, typ)
		}
		str := func(k string) string { v, _ := b[k].(string); return v }
		a := Alert{Name: str("alertname"), Namespace: str("namespace"), Severity: str("severity"), Status: str("status")}
		days, err := eventlog.DaysWithSubject(ctx, tx, Type, Subject(a.Name, a.Namespace), at.Add(-chronicLookback))
		if err != nil {
			return err
		}
		key = r.LetterKey(eventID, a, at, days)
		return nil
	})
	return key, err
}
