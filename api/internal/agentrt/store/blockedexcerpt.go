package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// What a blocked task's card quotes of its last check (TD-386): the check,
// and the part of its report that says why it failed.

type checkSummary struct {
	method, verdict, attempt string
	round                    int
	excerpt                  checkExcerpt // what the card quotes of the check's report
}

// checkExcerpt is what a card quotes of a check's report: why the check
// failed, where the report says it (TD-386), else the report's start.
type checkExcerpt struct {
	lead    string // what the text is, never cut: "Its report ends:", or the failed command and "Its output ends:"
	text    string // the quote, "" for none
	fromEnd bool   // the text is a report's or an output's end: it is cut from its front, so its last line stays
}

// commandRunes is the most of a failed command's argv a card quotes.
const commandRunes = 300

// reviewLines is how many lines before a review's last line a card quotes.
const reviewLines = 8

// checkExcerptOf is what a card quotes of report, a check's by method:
//   - deterministic, agentd's verify.json (which stops at the first failure,
//     so the failed command is its last): the first command that exited
//     non-zero or timed out and the end of its output; with none, the
//     report's reason;
//   - review, whose last line is its VERDICT: the report's last lines;
//   - else (a gate, a report that is not verify.json): the report's start.
func checkExcerptOf(method string, report []byte) checkExcerpt {
	text := strings.TrimSpace(strings.ToValidUTF8(string(report), "?"))
	if text == "" {
		return checkExcerpt{}
	}
	switch method {
	case "deterministic":
		var v struct {
			Commands []struct {
				Argv     []string `json:"argv"`
				ExitCode int      `json:"exit_code"`
				TimedOut bool     `json:"timed_out"`
				Tail     string   `json:"tail"`
			} `json:"commands"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal(report, &v) != nil {
			break
		}
		for _, c := range v.Commands {
			if c.ExitCode == 0 && !c.TimedOut {
				continue
			}
			lead := fmt.Sprintf("First failed command: %s exited %d", oneLineRunes(strings.Join(c.Argv, " "), commandRunes), c.ExitCode)
			if c.TimedOut {
				lead += ", timed out"
			}
			if tail := strings.TrimRight(strings.ToValidUTF8(c.Tail, "?"), " \t\r\n"); tail != "" {
				return checkExcerpt{lead: lead + ".\nIts output ends:", text: tail, fromEnd: true}
			}
			return checkExcerpt{lead: lead + "; it wrote nothing."}
		}
		if reason := strings.TrimSpace(strings.ToValidUTF8(v.Reason, "?")); reason != "" {
			return checkExcerpt{lead: "No command failed; its reason:", text: reason}
		}
	case "review":
		lines := strings.Split(text, "\n")
		return checkExcerpt{lead: "Its report ends:", text: strings.Join(lines[max(0, len(lines)-reviewLines-1):], "\n"), fromEnd: true}
	}
	return checkExcerpt{lead: "Its report begins:", text: text}
}

// lastCheck is the task's last verification and what its card quotes of its
// report.
func lastCheck(ctx context.Context, tx pgx.Tx, taskID string) (*checkSummary, error) {
	var c checkSummary
	var evidence string
	err := tx.QueryRow(ctx, `SELECT v.method, v.verdict, v.attempt_id, a.attempt_no, COALESCE(v.evidence_artifact_id, '')
	  FROM agentrt.verification v JOIN agentrt.attempt a ON a.attempt_id = v.attempt_id
	  WHERE v.task_id = $1 ORDER BY v.created_at DESC, v.verification_id DESC LIMIT 1`, taskID).
		Scan(&c.method, &c.verdict, &c.attempt, &c.round, &evidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if evidence != "" {
		var body []byte
		// The whole report (at most maxArtifactBytes): a check says why it
		// failed at its end (TD-386).
		if err = tx.QueryRow(ctx, `SELECT body FROM agentrt.artifact_blob WHERE artifact_id = $1`, evidence).Scan(&body); err != nil {
			return nil, err
		}
		c.excerpt = checkExcerptOf(c.method, body)
	}
	return &c, nil
}
