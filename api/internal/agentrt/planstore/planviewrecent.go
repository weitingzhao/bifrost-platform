package planstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/strictjson"
)

type viewRecentCursor struct {
	Root    string           `json:"root"`
	AsOf    string           `json:"as_of"`
	Streams map[string]int64 `json:"streams"`
	Before  int64            `json:"before"`
}

// recentRead is a view's request for its Recent changes, made from its cursor
// before the work is read, because the same read of the work answers the page
// and the coverage count. A cursor that does not hold is refused where Recent
// changes is built, after every other part of the view has been read.
type recentRead struct {
	cursor viewRecentCursor
	first  bool
	bounds []byte
	err    error
}

func parseRecentCursor(root, cursor string, now time.Time) recentRead {
	r := recentRead{cursor: viewRecentCursor{Root: root, AsOf: timeText(now), Streams: map[string]int64{}}, first: cursor == ""}
	if r.first {
		return r
	}
	c := &r.cursor
	if len(cursor) > 128*1024 {
		r.err = eventlog.Invalid("recent cursor too large")
		return r
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		r.err = eventlog.Invalid("invalid recent cursor")
		return r
	}
	if err = strictjson.JSON(raw, c); err != nil {
		r.err = err
		return r
	}
	if c.Root != root || len(c.Streams) > 2000 || c.Before < 1 {
		r.err = eventlog.Invalid("recent cursor belongs to another root or is invalid")
		return r
	}
	if _, err = time.Parse(time.RFC3339Nano, c.AsOf); err != nil {
		r.err = eventlog.Invalid("invalid cursor time")
		return r
	}
	r.bounds, r.err = json.Marshal(c.Streams)
	return r
}

// workPage is the page of the work's events this request needs. The merge
// keeps the newest RecentLimit of both sides, so RecentLimit of each is enough.
func (r recentRead) workPage() RecentWorkPage {
	if r.err != nil {
		return RecentWorkPage{}
	}
	return RecentWorkPage{Bounds: r.bounds, Before: r.cursor.Before, Limit: planreadings.RecentLimit}
}

// A continuation fixes each stream's committed sequence, not a global
// event-id high water mark: a late commit cannot enter an older page. The
// plan's own events and its work's are two reads of tx, one snapshot.
func planViewRecent(ctx context.Context, tx pgx.Tx, root string, r recentRead, work RootActivity) (json.RawMessage, string, string, error) {
	if r.err != nil {
		return nil, "", "", r.err
	}
	c := r.cursor
	var own []planreadings.RecentEvent
	var err error
	if r.first {
		var streams map[string]int64
		if streams, own, err = planreadings.RecentFirstPage(ctx, tx, PlanStream(root), root); err != nil {
			return nil, "", "", err
		}
		c.Streams = planreadings.MergeRecentStreams(streams, work.Streams)
	} else if own, err = planreadings.RecentEvents(ctx, tx, PlanStream(root), root, r.bounds, c.Before); err != nil {
		return nil, "", "", err
	}
	events := planreadings.MergeRecent(own, work.Recent)
	next := ""
	if len(events) > 20 {
		events = events[:20]
		c.Before = events[19].ID
		raw, err := json.Marshal(c)
		if err != nil {
			return nil, "", "", err
		}
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	out := []json.RawMessage{}
	for _, e := range events {
		out = append(out, e.Raw)
	}
	raw, err := json.Marshal(out)
	return raw, next, c.AsOf, err
}
