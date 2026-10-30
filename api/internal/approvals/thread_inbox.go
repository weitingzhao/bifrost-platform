package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/approvalnotify"
	"github.com/weitingzhao/bifrost-platform/api/internal/threadinbox"
)

// SetThreadInbox installs the runtime sender; no HTTP/MCP contract changes.
func (s *Service) SetThreadInbox(send threadinbox.Sender) {
	s.mu.Lock()
	s.threadInbox = send
	s.mu.Unlock()
}

// RetryThreadInbox is also used by the existing worker loop. Decisions are
// already committed; neither a failed message nor a failed receipt changes one.
func (s *Service) RetryThreadInbox(ctx context.Context) {
	s.mu.Lock()
	send := s.threadInbox
	s.mu.Unlock()
	if send == nil {
		return
	}
	if err := s.retryThreadInbox(ctx, send); err != nil {
		slog.Warn("approval thread inbox: will retry", "error", err)
	}
}

func (s *Service) retryThreadInbox(ctx context.Context, send threadinbox.Sender) error {
	return s.inboxScan.Run(ctx, func(ctx context.Context) ([]threadinbox.Conclusion, error) {
		doc, err := s.store.readContext(ctx)
		if err != nil {
			return nil, err
		}
		var items []threadinbox.Conclusion
		for _, a := range doc.Approvals {
			switch a.Status {
			case StatusExecuted, StatusFailed, StatusRejected, StatusExpired, StatusUnknown:
			default:
				continue
			}
			to := threadinbox.Recipient(a.RequesterThread)
			if to == "" {
				continue
			}
			wasUnknown := a.Resolution != nil || a.Execution != nil && a.Execution.LateResult
			if (a.Status == StatusExecuted || a.Status == StatusFailed) && wasUnknown {
				// LateResult/Resolution are existing business evidence of the
				// earlier unknown state. Never attach the later result/exit code.
				unknown := a
				unknown.Status, unknown.Result, unknown.Execution = StatusUnknown, nil, nil
				items = append(items, threadinbox.Conclusion{Key: "approval:" + a.ID + ":unknown", To: to, Body: approvalConclusion(unknown)})
			}
			items = append(items, threadinbox.Conclusion{Key: "approval:" + a.ID + ":" + a.Status, To: to, Body: approvalConclusion(a)})
		}
		return items, nil
	}, send)
}

func approvalConclusion(a Approval) string {
	lines := []string{fmt.Sprintf("Approval #%d (%s)", a.Number, a.ID),
		"Action: " + threadinbox.Text(a.Action, 120), "Environment: " + threadinbox.Text(a.Env, 80),
		"Conclusion: " + a.Status}
	if a.Execution != nil && a.Execution.ExitCode != nil {
		lines = append(lines, fmt.Sprintf("Exit code: %d", *a.Execution.ExitCode))
	}
	if summary := inboxResult(a.Result); summary != "" {
		lines = append(lines, "Result: "+summary)
	}
	lines = append(lines, approvalnotify.ConsoleClickPrefix+url.QueryEscape(a.ID))
	return strings.Join(lines, "\n")
}

var resultIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]{0,199}$`)

// Explicitly allow identifiers and status fields, never params, credentials,
// lease ids, arbitrary nested results, output tails or free-form messages.
func inboxResult(result any) string {
	raw, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return ""
	}
	var parts []string
	add := func(key string, v any) {
		if text, ok := v.(string); ok && resultIdentifier.MatchString(text) && Redact(text) == text {
			parts = append(parts, key+"="+text)
		}
	}
	for _, key := range []string{"grant_id", "run_name", "name", "namespace", "status", "revision"} {
		add(key, fields[key])
	}
	if ok, exists := fields["ok"].(bool); exists {
		parts = append(parts, fmt.Sprintf("ok=%t", ok))
	}
	if run, ok := fields["run"].(map[string]any); ok {
		for _, key := range []string{"name", "namespace", "pipeline", "status", "revision"} {
			add("run."+key, run[key])
		}
	} else {
		add("run", fields["run"])
	}
	return strings.Join(parts, ", ")
}
