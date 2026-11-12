package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/landing"
)

// repoStopped: a failed{denied} stopped the repository and no Owner's
// resume has answered that stop's generation yet (B6, CUT §11.4).
func repoStopped(ctx context.Context, tx pgx.Tx, stream string) (bool, error) {
	var stopped, resumed int
	err := tx.QueryRow(ctx, `SELECT
	    COALESCE(max((body_json->'landing_stop'->>'generation')::int), 0),
	    COALESCE(max((body_json->'landing_resume'->>'generation')::int), 0)
	  FROM agentrt.event WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&stopped, &resumed)
	return stopped > resumed, err
}

// stopRepo records the repository stopped by op's denied push, in the
// transaction that ends it failed{denied}: the stop and the ending commit
// together or not at all (B6).
func stopRepo(ctx context.Context, tx pgx.Tx, stream, repo, op, root string) error {
	var gen int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max((body_json->'landing_stop'->>'generation')::int), 0) + 1 FROM agentrt.event
	  WHERE event_stream_id = $1 AND event_type = 'note'`, stream).Scan(&gen); err != nil {
		return err
	}
	_, err := Append(ctx, tx, Event{Stream: stream, Type: "note", Actor: "controller", TaskID: root,
		Body: map[string]any{"text": "Landing stopped for the repository: a push to main was denied; the Owner resumes it",
			"landing_stop": map[string]any{"repo": repo, "op": op, "root": root, "generation": gen}}})
	return err
}

// badLandingStop is why a stop or resume body is refused, or "".
func badLandingStop(name string, v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return name + " is not an object"
	}
	if r, _ := m["repo"].(string); r == "" {
		return name + " needs repo"
	}
	g, ok := m["generation"].(int64)
	if !ok {
		if f, isF := m["generation"].(float64); isF && f == float64(int64(f)) {
			g, ok = int64(f), true
		} else if i, isI := m["generation"].(int); isI {
			g, ok = int64(i), true
		}
	}
	if !ok || g < 1 {
		return name + " generation is not a positive integer"
	}
	if name == "landing_stop" {
		if op, _ := m["op"].(string); op == "" {
			return "landing_stop needs op"
		}
		if !landingRoot.MatchString(fmt.Sprint(m["root"])) {
			return "landing_stop root is not a task"
		}
	}
	return ""
}

// checkLandingStopAppend: only the controller stops or resumes, on the
// repository stream, never as a landing record.
func checkLandingStopAppend(ev Event) error {
	_, stop := ev.Body["landing_stop"]
	_, resume := ev.Body["landing_resume"]
	if !stop && !resume {
		return nil
	}
	if ev.Type != "note" || ev.Actor != "controller" || !landing.RepoStream(ev.Stream) {
		return fmt.Errorf("a landing stop or resume is written by the controller on the repository stream")
	}
	if _, exists := ev.Body["landing"]; exists {
		return fmt.Errorf("a landing stop or resume is not a landing record")
	}
	return nil
}

func authRepo(auth Authorization) []string {
	if auth.Repo == "" {
		return nil
	}
	return []string{auth.Repo}
}
