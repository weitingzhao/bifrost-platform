package server

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
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

// diskPage pages the Owner's phone that a host's disk went below the floor or
// is back above (TD-415), as the other runtime pages go: through the alert
// relay, opening For you.
func diskPage(ctx context.Context, title, body, id string) error {
	return approvalnotify.Notify(ctx, approvalnotify.Message{
		Title: title, Message: body, ClickURL: ConsoleNeedsYou, Fallback: "Look · host disk", Kind: "agentrt_disk", ID: id,
	})
}
