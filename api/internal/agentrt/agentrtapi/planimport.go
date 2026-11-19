package agentrtapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/externalwork"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planimport"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planstore"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/recorderimport"
)

// The plan import route stays here when the plan's other routes went to
// planapi (2026-11-19): by the request's format it reads a plan
// (planimport) or a recorder log, which the lineage prepares
// (externalwork, recorderimport), and the plan domain imports no lineage
// package.

type planImportBody struct {
	IdempotencyKey string          `json:"idempotency_key"`
	Reason         string          `json:"reason"`
	Request        json.RawMessage `json:"request"`
}

// proposePlanImport is POST /plan/imports: a plan's first import
// (CUT-plan-import-2026-10-19.md §4 A), or a recorder log's
// (agentrt.recorder_import/v1, CUT-gate-b-2026-10-19.md §2 item 3 A), by the
// request's format; tried and rolled back, kept, and put to the Owner on one
// decision card; the controller lands it once he answers Import.
func (a *API) proposePlanImport(w http.ResponseWriter, r *http.Request) {
	var b planImportBody
	if !decode(w, r, &b) {
		return
	}
	// The request is read whole before the writer, as when it was one type
	// (review 402 C2).
	var head struct {
		Format string `json:"format"`
	}
	_ = json.Unmarshal(b.Request, &head)
	var recorder recorderimport.Request
	var plan planimport.Request
	if head.Format == recorderimport.Format {
		if !strictly(w, b.Request, &recorder) {
			return
		}
	} else if !strictly(w, b.Request, &plan) {
		return
	}
	who, err := writer(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	var out map[string]any
	if head.Format == recorderimport.Format {
		// The lineage reads, checks and keeps the log; the plan proposes its
		// import (2026-11-17: it left the store along this line).
		var log externalwork.PreparedRecorderLog
		if log, err = externalwork.PrepareRecorderImport(recorder); err == nil {
			out, err = planstore.ProposeRecorderImport(r.Context(), a.DB, who, b.IdempotencyKey, b.Reason, planstore.RecorderLog(log), a.Ask)
		}
	} else {
		out, err = planstore.ProposePlanImport(r.Context(), a.DB, who, b.IdempotencyKey, b.Reason, plan, a.Ask)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// strictly reads a request that came as raw JSON into v as decode reads a
// body, with decode's answer: a field v does not have is refused (400).
func strictly(w http.ResponseWriter, raw json.RawMessage, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "body: "+err.Error())
		return false
	}
	return true
}
