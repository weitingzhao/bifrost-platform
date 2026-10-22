package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// A host's free disk (TD-415 layer 2; the Owner 2026-10-21 「agentd 心跳带剩余
// 磁盘，低于下限不派新活并告警，尝试结束后清工作目录」). agentd's claim, the
// host's heartbeat (design v5 §5.2: the long poll replaced the host
// heartbeat route), carries the free bytes of the volume the host's root is
// on and of the one its AGENTD_ROOT is on. Below the floor on either the
// claim leases nothing, as for a host with no free slot, and the poll still
// counts as the host's liveness; the platform records a policy_alert (rule
// disk_low) on the host's stream when a claim first reports it below, and a
// note when one reports it at or above again, once each way. Each record is
// paged to the Owner by the controller (DiskPages), which marks it paged
// once the relay took it or has no target, and tries again later when it
// failed (review 497 A4). A claim with neither field (an agentd from before
// this) is not judged.

// DefaultDiskFloor is the floor when the platform's configuration names none:
// 15 GiB, below which a test database, a Go build or a clone can fail.
const DefaultDiskFloor int64 = 15 << 30

// diskLowPhrase is the alert's phrase.
const diskLowPhrase = "a host's free disk is below the floor"

// diskBackAbove starts the note that a host's free disk is at or above the
// floor again (after its host).
const diskBackAbove = " has free disk at or above the floor again"

// HostDisk is the free disk a claim reports, in bytes: of the volume the
// host's root is on, and of the one agentd's root (AGENTD_ROOT) is on.
type HostDisk struct {
	Host, Root int64
}

// Below says whether either volume has less free than floor (floor 0 or
// less: DefaultDiskFloor).
func (d HostDisk) Below(floor int64) bool {
	if floor <= 0 {
		floor = DefaultDiskFloor
	}
	return d.Host < floor || d.Root < floor
}

func gib(b int64) string { return fmt.Sprintf("%.1f GiB", float64(b)/(1<<30)) }

// RecordHostDisk records a change of host's free disk against floor: a
// policy_alert when it went below, a note when it is back above. It returns
// "below" or "above" when it recorded one, "" when nothing changed. The
// host's stream is locked before its latest record is read, so two claims of
// one host record a change once.
func RecordHostDisk(ctx context.Context, db DB, host string, d HostDisk, floor int64) (string, error) {
	if floor <= 0 {
		floor = DefaultDiskFloor
	}
	below := d.Below(floor)
	changed := ""
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		changed = ""
		stream := "host:" + host
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		var kind string
		err := tx.QueryRow(ctx, `SELECT event_type FROM agentrt.event WHERE event_stream_id = $1 AND actor_id = 'controller' AND (
		    (event_type = 'policy_alert' AND body_json->>'rule' = 'disk_low')
		    OR (event_type = 'note' AND starts_with(body_json->>'text', $2)))
		  ORDER BY seq DESC LIMIT 1`, stream, host+diskBackAbove).Scan(&kind)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		open := kind == "policy_alert"
		var now time.Time
		if err := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		facts := fmt.Sprintf("at %s it reported %s free on its root's volume and %s on agentd's, the floor %s",
			now.UTC().Format(time.RFC3339), gib(d.Host), gib(d.Root), gib(floor))
		switch {
		case below && !open:
			text := fmt.Sprintf("Host %s: %s. While its claims report less than the floor, it is given no new work.", host, facts)
			art, err := PutArtifact(ctx, tx, "controller", "log_tail", "text/plain; charset=utf-8", []byte(text))
			if err != nil {
				return err
			}
			if _, err = Append(ctx, tx, Event{Stream: stream, Type: "policy_alert", Actor: "controller",
				Body: map[string]any{"rule": "disk_low", "source": "controller", "phrase": diskLowPhrase, "artifact_id": art}}); err != nil {
				return err
			}
			changed = "below"
		case !below && open:
			if _, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller",
				Body: map[string]any{"text": host + diskBackAbove + ": " + facts +
					". Disk no longer keeps work from it; its other conditions (slots, stops, drains) still apply."}}); err != nil {
				return err
			}
			changed = "above"
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return changed, nil
}

// DiskPage is a record of a host's disk to page the Owner with: the event,
// its host, whether the host went below or back above, the text and when.
type DiskPage struct {
	EventID     int64
	Host, Turn  string // Turn: "below" or "above"
	Title, Text string
	At          time.Time
}

// diskPaged starts the note that a disk record was paged (after it, the
// event's id); its result follows.
const diskPaged = "Paged the Owner of event "

// diskPageWindow bounds how far back records are paged: an older one is
// past being news.
const diskPageWindow = 7 * 24 * time.Hour

// UnpagedDiskChanges are the disk records of the last week no note says were
// paged, oldest first, at most limit.
func UnpagedDiskChanges(ctx context.Context, db DB, limit int) ([]DiskPage, error) {
	var out []DiskPage
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT e.event_id, substr(e.event_stream_id, 6), e.event_type, e.at,
		    COALESCE(convert_from(b.body, 'UTF8'), e.body_json->>'text', '')
		  FROM agentrt.event e LEFT JOIN agentrt.artifact_blob b ON b.artifact_id = e.body_json->>'artifact_id'
		  WHERE e.event_type IN ('policy_alert', 'note') AND e.at >= clock_timestamp() - make_interval(secs => $1)
		    AND e.event_stream_id LIKE 'host:%' AND e.actor_id = 'controller'
		    AND ((e.event_type = 'policy_alert' AND e.body_json->>'rule' = 'disk_low')
		      OR (e.event_type = 'note' AND starts_with(e.body_json->>'text', substr(e.event_stream_id, 6) || $2)))
		    AND NOT EXISTS (SELECT 1 FROM agentrt.event p WHERE p.event_stream_id = e.event_stream_id AND p.event_type = 'note'
		      AND p.event_id > e.event_id AND starts_with(p.body_json->>'text', $3 || e.event_id || ':'))
		  ORDER BY e.event_id LIMIT $4`, diskPageWindow.Seconds(), diskBackAbove, diskPaged, limit)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (DiskPage, error) {
			var p DiskPage
			var kind string
			err := r.Scan(&p.EventID, &p.Host, &kind, &p.At, &p.Text)
			p.Turn, p.Title = "above", "Host "+p.Host+" has room on disk again"
			if kind == "policy_alert" {
				p.Turn, p.Title = "below", "Host "+p.Host+" is low on disk"
			}
			return p, err
		})
		return err
	})
	return out, err
}

// MarkDiskPaged records that p was paged with result (the relay's: sent, or
// skipped when it has no target), once.
func MarkDiskPaged(ctx context.Context, db DB, p DiskPage, result string) error {
	return InTx(ctx, db, func(tx pgx.Tx) error {
		stream := "host:" + p.Host
		if err := lockStream(ctx, tx, stream); err != nil {
			return err
		}
		prefix := diskPaged + strconv.FormatInt(p.EventID, 10) + ":"
		var seen bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'
		  AND event_id > $2 AND starts_with(body_json->>'text', $3))`, stream, p.EventID, prefix).Scan(&seen); err != nil || seen {
			return err
		}
		_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", Body: map[string]any{"text": prefix + " " + result}})
		return err
	})
}
