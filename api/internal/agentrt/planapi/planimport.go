package planapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
)

// The plan import route came here from agentrtapi (planimport.go,
// 2026-11-19), the same code: by the request's format it reads a plan
// (planimport) or a recorder log. The lineage reads, checks and keeps a
// recorder log (externalwork, recorderimport); the plan proposes its import.
// The plan domain imports no lineage package: the assembly binds the
// lineage's part (Routes.RecorderLogs).

// RecorderLogs is the lineage's part of a plan import (bound by the
// assembly): a request in its format is a recorder log's.
type RecorderLogs interface {
	// Format is the format a recorder log's request names.
	Format() string
	// Request is an empty request of that format, which the body's request
	// is read into as strictly as a body.
	Request() any
	// Prepare reads, checks and keeps the log of a request read into what
	// Request gave, as the plan's proposal of its import needs it.
	Prepare(request any) (planstore.RecorderLog, error)
}

type planImportBody struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Reason         string          `json:"reason"`
	Request        json.RawMessage `json:"request"`
}

// HandleProposePlanImport is POST /plan/imports: a plan's first import
// (CUT-plan-import-2026-10-19.md §4 A), or a recorder log's
// (agentrt.recorder_import/v1, CUT-gate-b-2026-10-19.md §2 item 3 A), by the
// request's format; tried and rolled back, kept, and put to the Owner on one
// decision card; the controller lands it once he answers Import.
func (rt Routes) HandleProposePlanImport(w http.ResponseWriter, r *http.Request) {
	h := rt.Host
	var b planImportBody
	if !h.Decode(w, r, &b) {
		return
	}
	// The request is read whole before the writer, as when it was one type
	// (review 402 C2).
	var head struct {
		Format string `json:"format"`
	}
	_ = json.Unmarshal(b.Request, &head)
	recorderLog := head.Format == rt.RecorderLogs.Format()
	var recorder any
	var plan planimport.Request
	if recorderLog {
		recorder = rt.RecorderLogs.Request()
		if !rt.strictly(w, b.Request, recorder) {
			return
		}
	} else if !rt.strictly(w, b.Request, &plan) {
		return
	}
	who, err := h.Writer(r)
	if err != nil {
		h.Fail(w, err)
		return
	}
	var out map[string]any
	if recorderLog {
		// The lineage reads, checks and keeps the log; the plan proposes its
		// import (2026-11-17: it left the store along this line).
		var log planstore.RecorderLog
		if log, err = rt.RecorderLogs.Prepare(recorder); err == nil {
			out, err = planstore.ProposeRecorderImport(r.Context(), h.DB(), who, b.IdempotencyKey, b.Reason, log, h.Ask())
		}
	} else {
		out, err = planstore.ProposePlanImport(r.Context(), h.DB(), who, b.IdempotencyKey, b.Reason, plan, h.Ask())
	}
	if err != nil {
		h.Fail(w, err)
		return
	}
	h.JSON(w, http.StatusOK, out)
}

// strictly reads a request that came as raw JSON into v as Decode reads a
// body, with Decode's answer: a field v does not have is refused (400).
func (rt Routes) strictly(w http.ResponseWriter, raw json.RawMessage, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		rt.Host.Error(w, http.StatusBadRequest, "bad_request", "body: "+err.Error())
		return false
	}
	return true
}
