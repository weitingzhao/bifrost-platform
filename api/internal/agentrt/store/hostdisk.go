package store

// Historical dispatch disk values retained for event and boundary readers.
// Claims no longer allocate work or page the Owner on disk changes.

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
