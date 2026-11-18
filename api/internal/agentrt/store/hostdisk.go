package store

// Historical dispatch disk values retained for event and boundary readers.
// Claims no longer allocate work or page the Owner on disk changes.

// DefaultDiskFloor is the floor when the platform's configuration names none:
// 15 GiB, below which a test database, a Go build or a clone can fail.
const DefaultDiskFloor int64 = 15 << 30
