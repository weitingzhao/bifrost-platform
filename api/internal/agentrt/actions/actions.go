// Package actions keeps, for 24 hours, what an attempt's executor did: the
// files it read and edited, the commands it ran and what they returned
// (W-31 2.3; Owner 2026-10-18: heartbeats carry actions, kept in redis-agents
// only, PostgreSQL does not keep them). The Console's "now" line and an
// attempt's action stream read them; checkpoints stay the evidence that lasts.
//
// The format is closed: four kinds, a target, a time, an exit code for the
// kinds that have one. A model's own reasoning is not an action and has no
// field here (Owner 2026-10-16: show actions, not raw thinking).
package actions

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"
)

// Kind is what the executor did.
type Kind string

const (
	Read   Kind = "read"   // read a file; Target is its path
	Edit   Kind = "edit"   // wrote or changed a file; Target is its path
	Run    Kind = "run"    // started a command; Target is the command line
	Result Kind = "result" // a command ended; Target is the command line, Code its exit code
)

var kinds = map[Kind]bool{Read: true, Edit: true, Run: true, Result: true}

// Limits of one heartbeat and of an attempt's stream.
const (
	MaxPerBeat   = 50             // a heartbeat keeps its newest 50 actions
	MaxTarget    = 400            // runes of a target; longer ones are cut and marked
	MaxStream    = 500            // approximate trimming of an attempt's stream
	TTL          = 24 * time.Hour // an attempt's stream expires after its last action
	maxClockSkew = 5 * time.Minute
)

// Action is one thing the executor did.
type Action struct {
	Kind      Kind      `json:"kind"`
	Target    string    `json:"target"`
	At        time.Time `json:"at"`
	Code      *int      `json:"code,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
}

// Entry is an action as stored, with its stream id (the order and the cursor).
type Entry struct {
	ID string `json:"id"`
	Action
}

// ErrInvalid is an action outside the format; the route answers 422.
var ErrInvalid = errors.New("actions: not an action")

// Normalize checks a heartbeat's actions against the closed format, cuts long
// targets (marking them), and keeps the newest MaxPerBeat. It returns how many
// older ones it dropped.
func Normalize(in []Action, now time.Time) ([]Action, int, error) {
	out := make([]Action, 0, len(in))
	for i, a := range in {
		if !kinds[a.Kind] {
			return nil, 0, fmt.Errorf("%w: action %d has kind %q", ErrInvalid, i, a.Kind)
		}
		if a.Target == "" || !utf8.ValidString(a.Target) {
			return nil, 0, fmt.Errorf("%w: action %d has no readable target", ErrInvalid, i)
		}
		if a.At.IsZero() || a.At.After(now.Add(maxClockSkew)) {
			return nil, 0, fmt.Errorf("%w: action %d has no time or one in the future", ErrInvalid, i)
		}
		if a.Code != nil && a.Kind != Result {
			return nil, 0, fmt.Errorf("%w: action %d of kind %s carries an exit code", ErrInvalid, i, a.Kind)
		}
		if utf8.RuneCountInString(a.Target) > MaxTarget {
			a.Target = string([]rune(a.Target)[:MaxTarget])
			a.Truncated = true
		}
		a.At = a.At.UTC()
		out = append(out, a)
	}
	dropped := 0
	if len(out) > MaxPerBeat {
		dropped = len(out) - MaxPerBeat
		out = out[dropped:]
	}
	return out, dropped, nil
}

var attemptID = regexp.MustCompile(`^a_[0-9A-HJKMNP-TV-Z]{26}$`)

// Store reads and writes attempts' streams in redis-agents.
type Store struct {
	Redis  *redis.Client
	Prefix string // "agrt:" in PROD and STG, "agrt-dev:" on DEV's shared Redis
}

func (s *Store) key(attempt string) (string, error) {
	if !attemptID.MatchString(attempt) {
		return "", fmt.Errorf("%w: attempt id %q", ErrInvalid, attempt)
	}
	return s.Prefix + "act:" + attempt, nil
}

// Append adds normalized actions to the attempt's stream and sets it to
// expire TTL after this write.
func (s *Store) Append(ctx context.Context, attempt string, acts []Action) error {
	if len(acts) == 0 {
		return nil
	}
	k, err := s.key(attempt)
	if err != nil {
		return err
	}
	pipe := s.Redis.TxPipeline()
	for _, a := range acts {
		v := map[string]any{"kind": string(a.Kind), "target": a.Target, "at": a.At.UTC().Format(time.RFC3339Nano)}
		if a.Code != nil {
			v["code"] = strconv.Itoa(*a.Code)
		}
		if a.Truncated {
			v["truncated"] = "1"
		}
		pipe.XAdd(ctx, &redis.XAddArgs{Stream: k, MaxLen: MaxStream, Approx: true, Values: v})
	}
	pipe.Expire(ctx, k, TTL)
	_, err = pipe.Exec(ctx)
	return err
}

// Read returns up to limit actions after the stream id after ("" for the
// start), oldest first.
func (s *Store) Read(ctx context.Context, attempt, after string, limit int) ([]Entry, error) {
	k, err := s.key(attempt)
	if err != nil {
		return nil, err
	}
	start := "-"
	if after != "" {
		start = "(" + after
	}
	msgs, err := s.Redis.XRangeN(ctx, k, start, "+", int64(limit)).Result()
	if err != nil {
		return nil, err
	}
	return entries(msgs)
}

// Last returns the attempt's newest action, if it has one.
func (s *Store) Last(ctx context.Context, attempt string) (Entry, bool, error) {
	k, err := s.key(attempt)
	if err != nil {
		return Entry{}, false, err
	}
	msgs, err := s.Redis.XRevRangeN(ctx, k, "+", "-", 1).Result()
	if err != nil || len(msgs) == 0 {
		return Entry{}, false, err
	}
	es, err := entries(msgs)
	if err != nil {
		return Entry{}, false, err
	}
	return es[0], true, nil
}

func entries(msgs []redis.XMessage) ([]Entry, error) {
	out := make([]Entry, 0, len(msgs))
	for _, m := range msgs {
		e := Entry{ID: m.ID}
		e.Kind = Kind(str(m.Values["kind"]))
		e.Target = str(m.Values["target"])
		at, err := time.Parse(time.RFC3339Nano, str(m.Values["at"]))
		if err != nil || !kinds[e.Kind] {
			return nil, fmt.Errorf("actions: stream entry %s is not an action", m.ID)
		}
		e.At = at
		if c, ok := m.Values["code"]; ok {
			n, err := strconv.Atoi(str(c))
			if err != nil {
				return nil, fmt.Errorf("actions: stream entry %s has code %v", m.ID, c)
			}
			e.Code = &n
		}
		e.Truncated = str(m.Values["truncated"]) == "1"
		out = append(out, e)
	}
	return out, nil
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
