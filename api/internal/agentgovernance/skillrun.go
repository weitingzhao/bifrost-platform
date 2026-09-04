package agentgovernance

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/remediation"
)

// Recording an outcome for a skill that runs outside the remediation runner.
//
// The trust matrix earns autonomy from recorded outcomes: PromotionThreshold
// consecutive successes at L1 make a skill eligible for L0. Every scope it could
// count came from an agent session the runner itself started, so a skill that
// runs anywhere else could never move. `research-loop-batch` is the case that
// exposed it — catalogued, scheduled, running green for weeks on its own
// CronJob, and pinned at 0 consecutive successes across 694 recorded jobs
// because nothing ever wrote one for it. It could not reach L0 by any amount of
// success; the gate was not strict, it was disconnected.
//
// This stays platform-generic on purpose: it takes a catalogued scope and an
// outcome. It does not know what the skill did, and must not learn.
type SkillRunRequest struct {
	Scope   string `json:"scope"`
	Status  string `json:"status"`
	Summary string `json:"summary,omitempty"`
	Error   string `json:"error,omitempty"`
}

// HandleRecordSkillRun records a finished run of a catalogued skill.
func (h *Handler) HandleRecordSkillRun(w http.ResponseWriter, r *http.Request) {
	var req SkillRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	scope, ok := canonicalScope(strings.TrimSpace(req.Scope))
	if !ok {
		// An unknown scope would be written and then never read — the trust
		// matrix only groups scopes the catalog declares.
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "unknown scope",
			"hint":  "scope must match an id or scope in config/agent-tasks.yaml",
		})
		return
	}

	status, statusOK := jobStatusFor(req.Status)
	if !statusOK {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": `status must be "done" or "failed"`,
		})
		return
	}

	principal := actuation.PrincipalFromContext(r.Context())
	now := time.Now().UTC()
	job := remediation.Job{
		ID:        fmt.Sprintf("skillrun-%s-%d", strings.ReplaceAll(scope, "/", "-"), now.UnixNano()),
		Phase:     remediation.Phase(status),
		Status:    status,
		Summary:   strings.TrimSpace(req.Summary),
		Error:     strings.TrimSpace(req.Error),
		Actor:     principal.Name,
		Scope:     scope,
		CreatedAt: now,
		UpdatedAt: now,
	}
	h.store.Put(job)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"job_id": job.ID,
		"scope":  scope,
		"status": string(status),
	})
}

func jobStatusFor(raw string) (remediation.JobStatus, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "done", "success", "succeeded":
		return remediation.JobDone, true
	case "failed", "failure", "error":
		return remediation.JobFailed, true
	}
	return "", false
}

// canonicalScope maps a catalog id or scope onto the scope the trust matrix
// groups by, and reports whether it is catalogued at all.
//
// Both spellings are accepted because callers reasonably reach for the name they
// can see — the matrix shows `research-loop-batch`, the catalog files it under
// `research.loop.batch`. Storing whichever arrived would write a record that is
// never read, which is precisely the disconnection this endpoint exists to end.
func canonicalScope(scope string) (string, bool) {
	if scope == "" {
		return "", false
	}
	for _, t := range TaskCatalog() {
		if t.Scope == scope || t.ID == scope {
			return t.Scope, true
		}
	}
	return "", false
}
