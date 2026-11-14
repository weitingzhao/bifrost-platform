package store_test

import (
	"testing"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

var owner = store.Writer{Actor: "owner", Token: "admin"}

func thread(t *testing.T, w *world) []store.Message {
	t.Helper()
	ms, err := store.Thread(ctx, w.rt, "owner", store.Seat, 50)
	ok(t, err)
	return ms
}
