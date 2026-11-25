package planstore

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// What the Owner says in the threads (Owner 2026-11-25 in the legislation
// thread, choosing automatic collection: 「我觉得B好，需要自动收集」). A session
// hook reads his own messages from the transcript and posts them; each is
// recorded once, as owner_said on owner-said:<transcript>, so the legislation
// line can classify them and count, round by round, whether the same pain
// comes back (LAW-1 s7, and the standards that waited on 「Owner 的吐槽进系统
// 的记录」). The text is his, kept in the platform's database, never in a
// repository.

// OwnerSaidMax is the most of one message kept; a longer one is cut there and
// marked truncated.
const OwnerSaidMax = 16 * 1024

// OwnerSaid is one message of the Owner's.
type OwnerSaid struct {
	// MessageID is the transcript record's uuid, the key a message is
	// recorded once by.
	MessageID string `json:"message_id"`
	// Transcript is the session's transcript id (the CLI session id).
	Transcript string `json:"transcript"`
	// Thread is the desktop thread id (local_…), when the hook knows it.
	Thread string    `json:"thread,omitempty"`
	Host   string    `json:"host,omitempty"`
	SaidAt time.Time `json:"said_at"`
	Text   string    `json:"text"`
	// Truncated is set when Text was cut at OwnerSaidMax.
	Truncated bool `json:"truncated,omitempty"`
}

var (
	ownerSaidID    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	ownerSaidHost  = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	ownerSaidLocal = regexp.MustCompile(`^local_[0-9a-f-]{8,64}$`)
)

// OwnerSaidStream is a transcript's stream of the Owner's messages.
func OwnerSaidStream(transcript string) string { return "owner-said:" + transcript }

func checkOwnerSaid(m *OwnerSaid) error {
	switch {
	case !ownerSaidID.MatchString(m.MessageID):
		return eventlog.Invalid("message_id is a transcript record's uuid")
	case !ownerSaidID.MatchString(m.Transcript):
		return eventlog.Invalid("transcript is a session id (uuid)")
	case m.Thread != "" && !ownerSaidLocal.MatchString(m.Thread):
		return eventlog.Invalid("thread is a local_… id")
	case m.Host != "" && !ownerSaidHost.MatchString(m.Host):
		return eventlog.Invalid("host is a short name")
	case m.SaidAt.IsZero():
		return eventlog.Invalid("said_at is required")
	case m.Text == "" || !utf8.ValidString(m.Text):
		return eventlog.Invalid("text is the message, UTF-8")
	}
	if len(m.Text) > OwnerSaidMax {
		cut := OwnerSaidMax
		for cut > 0 && !utf8.RuneStart(m.Text[cut]) {
			cut--
		}
		m.Text, m.Truncated = m.Text[:cut], true
	}
	return nil
}

// RecordOwnerSaid records each message not yet recorded and says how many it
// added; a message already recorded (by its message_id) is skipped, so the
// hook may post the same message again.
func RecordOwnerSaid(ctx context.Context, db eventlog.DB, actor string, msgs []OwnerSaid) (int, error) {
	if len(msgs) == 0 || len(msgs) > 200 {
		return 0, eventlog.Invalid("1 to 200 messages a call")
	}
	for i := range msgs {
		if err := checkOwnerSaid(&msgs[i]); err != nil {
			return 0, fmt.Errorf("message %d: %w", i, err)
		}
	}
	added := 0
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		added = 0
		for _, m := range msgs {
			have, err := eventlog.HasBody(ctx, tx, "owner_said", "", map[string]any{"message_id": m.MessageID})
			if err != nil {
				return err
			}
			if have {
				continue
			}
			body := map[string]any{"message_id": m.MessageID, "transcript": m.Transcript,
				"said_at": m.SaidAt.UTC().Format(time.RFC3339Nano), "text": m.Text}
			if m.Thread != "" {
				body["thread"] = m.Thread
			}
			if m.Host != "" {
				body["host"] = m.Host
			}
			if m.Truncated {
				body["truncated"] = true
			}
			if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: OwnerSaidStream(m.Transcript), Type: "owner_said", Actor: actor, Body: body}); err != nil {
				return err
			}
			added++
		}
		return nil
	})
	return added, err
}

// OwnerSaidSince lists the messages said at or after since, oldest first, at
// most limit (1 to 1000). A message is recorded after it is said, so the
// events recorded from since on hold every one said from since on.
func OwnerSaidSince(ctx context.Context, db eventlog.DB, since time.Time, limit int) ([]OwnerSaid, error) {
	if limit < 1 || limit > 1000 {
		limit = 1000
	}
	var out []OwnerSaid
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		rows, err := eventlog.OfType(ctx, tx, "owner_said", since, time.Now().Add(time.Hour))
		if err != nil {
			return err
		}
		for _, r := range rows {
			s := func(k string) string { v, _ := r.Body[k].(string); return v }
			at, err := time.Parse(time.RFC3339Nano, s("said_at"))
			if err != nil || at.Before(since) {
				continue
			}
			tr, _ := r.Body["truncated"].(bool)
			out = append(out, OwnerSaid{MessageID: s("message_id"), Transcript: s("transcript"), Thread: s("thread"), Host: s("host"),
				SaidAt: at, Text: s("text"), Truncated: tr})
		}
		return nil
	})
	sort.SliceStable(out, func(i, j int) bool { return out[i].SaidAt.Before(out[j].SaidAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, err
}
