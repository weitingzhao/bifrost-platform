package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
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

// diskPage pages the Owner's phone of a host's disk going below the floor
// or back above (TD-415), through the alert relay as the other runtime pages
// go, opening For you, and says how the relay took it: "sent", or "skipped"
// when it has no target; an error when it failed, for the controller to try
// again (DiskPages, review 497 A4).
func diskPage(ctx context.Context, title, body, id string) (string, error) {
	ds, err := notice.Send(ctx, notice.Message{Title: title, Message: body, ClickURL: ConsoleNeedsYou, Priority: 4,
		Fallback: "Look · host disk", Kind: "agentrt_disk", ID: id})
	if err != nil {
		return "", err
	}
	result := notice.ResultSkipped
	for _, d := range ds {
		switch d.Result {
		case notice.ResultFailed:
			return "", fmt.Errorf("the relay could not page %s: %s", d.Target, d.Error)
		case notice.ResultSkipped:
		default:
			result = d.Result
		}
	}
	return result, nil
}
