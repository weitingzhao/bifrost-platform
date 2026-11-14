package store

import (
	"time"
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

// DiskPage is a record of a host's disk to page the Owner with: the event,
// its host, whether the host went below or back above, the text and when.
type DiskPage struct {
	EventID     int64
	Host, Turn  string // Turn: "below" or "above"
	Title, Text string
	At          time.Time
}
