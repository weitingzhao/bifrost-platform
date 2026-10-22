package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// A host's free disk (TD-415 layer 2; the Owner 2026-10-21 「agentd 心跳带剩余
// 磁盘，低于下限不派新活并告警，尝试结束后清工作目录」). agentd's claim, the
// host's heartbeat (design v5 §5.2: the long poll replaced the host
// heartbeat route), carries the free bytes of the volume the host's root is
// on and of the one its AGENTD_ROOT is on. Below the floor on either the
// claim leases nothing, as for a host with no free slot, and the poll still
// counts as the host's liveness; the platform records a policy_alert (rule
// disk_low) on the host's stream when the host goes below, and a note when it
// is back above, once each way. A claim with neither field (an agentd from
// before this) is not judged.

// DefaultDiskFloor is the floor when the platform's configuration names none:
// 15 GiB, below which a test database, a Go build or a clone can fail.
const DefaultDiskFloor int64 = 15 << 30

// diskLowPhrase is the alert's phrase.
const diskLowPhrase = "a host's free disk is below the floor"

// diskBackAbove starts the note that a host's free disk is above the floor
// again (after its host).
const diskBackAbove = " has free disk above the floor again"

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
		facts := fmt.Sprintf("free %s on the host's root volume, %s on agentd's root volume, the floor %s", gib(d.Host), gib(d.Root), gib(floor))
		switch {
		case below && !open:
			text := fmt.Sprintf("Host %s: %s. It takes no new work until both are above the floor again.", host, facts)
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
				Body: map[string]any{"text": host + diskBackAbove + ": " + facts + "."}}); err != nil {
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
