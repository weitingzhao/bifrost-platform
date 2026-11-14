package server

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// diskFloorFromEnv is the free disk, in GiB, below which a host is given no
// new work (AGENTRT_DISK_FLOOR_GIB; TD-415): unset, 0, or not a whole number
// above 0 leaves the runtime's default (store.DefaultDiskFloor), and the log
// says which.
func diskFloorFromEnv() int64 {
	v := strings.TrimSpace(os.Getenv("AGENTRT_DISK_FLOOR_GIB"))
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 || n > 1<<20 {
		slog.Error("agentrt: AGENTRT_DISK_FLOOR_GIB is not a whole number of GiB above 0; the default floor holds", "value", v)
		return 0
	}
	return n << 30
}
