package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"
)

type viewRecentCursor struct {
	Root    string           `json:"root"`
	AsOf    string           `json:"as_of"`
	Streams map[string]int64 `json:"streams"`
	Before  int64            `json:"before"`
}

// A continuation fixes each stream's committed sequence, not a global
// event-id high water mark: a late commit cannot enter an older page.
func planViewRecent(ctx context.Context, tx pgx.Tx, root, cursor string, now time.Time) (json.RawMessage, string, string, error) {
	c := viewRecentCursor{Root: root, AsOf: timeText(now), Streams: map[string]int64{}}
	if cursor != "" {
		if len(cursor) > 128*1024 {
			return nil, "", "", invalid("recent cursor too large")
		}
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, "", "", invalid("invalid recent cursor")
		}
		if err = strictViewJSON(raw, &c); err != nil {
			return nil, "", "", err
		}
		if c.Root != root || len(c.Streams) > 2000 || c.Before < 1 {
			return nil, "", "", invalid("recent cursor belongs to another root or is invalid")
		}
		if _, err = time.Parse(time.RFC3339Nano, c.AsOf); err != nil {
			return nil, "", "", invalid("invalid cursor time")
		}
	} else {
		streams, err := planreadings.RecentStreams(ctx, tx, PlanStream(root), root)
		if err != nil {
			return nil, "", "", err
		}
		c.Streams = streams
	}
	bounds, err := json.Marshal(c.Streams)
	if err != nil {
		return nil, "", "", err
	}
	events, err := planreadings.RecentEvents(ctx, tx, PlanStream(root), root, bounds, c.Before)
	if err != nil {
		return nil, "", "", err
	}
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
