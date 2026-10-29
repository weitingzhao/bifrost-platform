package agentrtapi

import (
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/actuation"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

func (a *API) getArtifact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var media, kind string
	var body []byte
	var exam bool
	host := actuation.PrincipalFromContext(r.Context()).Role == actuation.RoleAgentd
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		// Whether the exams' stream names it is read for an Agent host only
		// (review 412 C1: the stream is read by its id, then its bodies).
		return tx.QueryRow(r.Context(), `SELECT a.media_type, a.artifact_kind, b.body,
		    $3 AND EXISTS (SELECT 1 FROM agentrt.event e WHERE e.event_stream_id = $2 AND e.body_json->>'artifact_id' = a.artifact_id)
		  FROM agentrt.artifact a JOIN agentrt.artifact_blob b USING (artifact_id) WHERE a.artifact_id = $1`, id, store.ExamsStream, host).
			Scan(&media, &kind, &body, &exam)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	// An Agent host reads no exam artifact: an answer's id can be known to a
	// host that handed it in, and answers stay off the host (CUT-chief-exam
	// §5; review 406 A1). An exam artifact is one of kind exam or one the
	// exams' stream names, whatever kind its first upload gave it (the same
	// bytes put earlier as "other", review 409 A1). agentd keeps the paper
	// it was handed.
	if host && (kind == "exam" || exam) {
		writeError(w, http.StatusForbidden, "forbidden", "an agentd token does not read exam artifacts")
		return
	}
	w.Header().Set("Content-Type", media)
	_, _ = w.Write(body)
}

func (a *API) getRow(w http.ResponseWriter, r *http.Request, q, id string) {
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error { return tx.QueryRow(r.Context(), q, id).Scan(&doc) })
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}

func (a *API) getTask(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(t) FROM agentrt.task t WHERE task_id = $1`, chi.URLParam(r, "id"))
}

func (a *API) getAttempt(w http.ResponseWriter, r *http.Request) {
	a.getRow(w, r, `SELECT to_jsonb(a) FROM agentrt.attempt a WHERE attempt_id = $1`, chi.URLParam(r, "id"))
}

// hiddenFrom are the streams a principal of a role may not read: an Agent
// host's agentd does not read the exams' records (capability_exams: which
// rules an answer failed). The cut keeps per-rule results off the host
// (CUT-chief-exam §5), and an executor reads what its agentd's user can
// (TD-377), the token included.
func hiddenFrom(r *http.Request) []string {
	if actuation.PrincipalFromContext(r.Context()).Role == actuation.RoleAgentd {
		return []string{store.ExamsStream}
	}
	return []string{}
}

// hiddenPrefixes start the streams no role reads by GET /events, in either
// form: a business issue's history (issue:BI-n) and its intake records
// (intake:…) are read only by the issue routes (DESIGN-crew-intake §5.3).
var hiddenPrefixes = []string{"issue:", "intake:"}

// streamHidden says whether r may not read stream by GET /events.
func streamHidden(r *http.Request, stream string) bool {
	if slices.Contains(hiddenFrom(r), stream) {
		return true
	}
	return slices.ContainsFunc(hiddenPrefixes, func(p string) bool { return strings.HasPrefix(stream, p) })
}

// getEvents lists a stream's events after a sequence number, at most 500.
func (a *API) getEvents(w http.ResponseWriter, r *http.Request) {
	stream := r.URL.Query().Get("stream")
	if stream == "" {
		a.getRecentEvents(w, r)
		return
	}
	if streamHidden(r, stream) {
		writeError(w, http.StatusForbidden, "forbidden", "this token may not read stream "+stream)
		return
	}
	var after int64
	if s := r.URL.Query().Get("after_seq"); s != "" {
		if _, err := fmt.Sscan(s, &after); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "after_seq is a number")
			return
		}
	}
	var doc []byte
	err := store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT COALESCE(jsonb_agg(to_jsonb(e) ORDER BY e.seq), '[]') FROM (
		  SELECT * FROM agentrt.event WHERE event_stream_id = $1 AND seq > $2 ORDER BY seq LIMIT 500) e`, stream, after).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(doc)
}
