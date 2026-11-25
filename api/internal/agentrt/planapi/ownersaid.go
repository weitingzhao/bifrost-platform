package planapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

type ownerSaidBody struct {
	Messages []planstore.OwnerSaid `json:"messages"`
}

// HandlePostOwnerSaid is POST /owner-said {messages}: the session hook posts
// the Owner's own messages from a transcript (Owner 2026-11-25, automatic
// collection); each is recorded once by its message_id, so posting the same
// message again changes nothing. Answers {added}.
func (rt Routes) HandlePostOwnerSaid(w http.ResponseWriter, r *http.Request) {
	var b ownerSaidBody
	if !rt.Host.Decode(w, r, &b) {
		return
	}
	// Recorded as the controller: the hook is the platform's collector, not
	// a thread speaking; where it came from is in each message (transcript,
	// thread, host), and the route takes a reporter's token only.
	added, err := planstore.RecordOwnerSaid(r.Context(), rt.Host.DB(), "controller", b.Messages)
	if err != nil {
		rt.Host.Fail(w, err)
		return
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"added": added})
}

// HandleGetOwnerSaid is GET /owner-said?since=<RFC 3339>&limit=<n>: what the
// Owner said from since on (default the last 7 days), oldest first, at most
// limit (default and most 1000). Read-only.
func (rt Routes) HandleGetOwnerSaid(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-7 * 24 * time.Hour)
	if s := r.URL.Query().Get("since"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			rt.Host.Error(w, http.StatusBadRequest, "invalid", "since is RFC 3339")
			return
		}
		since = t
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	msgs, err := planstore.OwnerSaidSince(r.Context(), rt.Host.DB(), since, limit)
	if err != nil {
		rt.Host.Logf("owner-said: %v", err)
		rt.Host.Error(w, http.StatusServiceUnavailable, "unavailable", "the Owner's words are unavailable")
		return
	}
	if msgs == nil {
		msgs = []planstore.OwnerSaid{}
	}
	rt.Host.JSON(w, http.StatusOK, map[string]any{"since": since.UTC(), "messages": msgs})
}
