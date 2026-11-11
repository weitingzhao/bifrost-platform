package agentrtapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/metrics"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/store"
)

// Keep the projection explicit: provenance and database identifiers are not part
// of the readings contract. PostgreSQL encodes numeric values as JSON numbers.
const metricProjection = `metric_id, definition_version, definition_sha256,
 scope_kind, scope_ref, label, window_kind, window_start, window_end, source,
 unit, samples, value, stats_json AS stats, conclusive`

type metricQuery struct {
	metric, scopeKind, scopeRef, label, windowKind string
	limit                                          int
}

func parseMetricQuery(r *http.Request) (metricQuery, error) {
	q := r.URL.Query()
	p := metricQuery{q.Get("metric"), q.Get("scope_kind"), q.Get("scope_ref"), q.Get("label"), q.Get("window_kind"), 30}
	if p.metric == "" {
		return p, errors.New("metric is required")
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			return p, errors.New("limit must be between 1 and 500")
		}
		p.limit = n
	}
	return p, nil
}

var readingRegistry = sync.OnceValues(metrics.Load)

// Stored conclusions remain authoritative for historical versions whose sample
// floors are no longer in the registry. For known versions enforce the floor
// even for readings supplied by importers.
func enforceMetricFloor(doc []byte) ([]byte, error) {
	var reading map[string]json.RawMessage
	if err := json.Unmarshal(doc, &reading); err != nil {
		return nil, err
	}
	var id string
	var version, samples int
	if err := json.Unmarshal(reading["metric_id"], &id); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(reading["definition_version"], &version); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(reading["samples"], &samples); err != nil {
		return nil, err
	}
	registry, err := readingRegistry()
	if err != nil {
		return nil, err
	}
	for _, d := range registry.Definitions() {
		if d.ID == id && d.Version == version && samples < d.MinSamples {
			reading["conclusive"] = json.RawMessage("false")
		}
	}
	return json.Marshal(reading)
}

func (a *API) metricReadings(w http.ResponseWriter, r *http.Request) {
	p, err := parseMetricQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	var doc []byte
	err = store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(r.Context(), `SELECT jsonb_build_object('readings', COALESCE(jsonb_agg(to_jsonb(x) ORDER BY window_end DESC, definition_version DESC, source), '[]'::jsonb)) FROM (
   SELECT `+metricProjection+` FROM agentrt.metric_reading
   WHERE metric_id=$1 AND ($2='' OR scope_kind=$2) AND ($3='' OR scope_ref=$3)
    AND ($4='' OR label=$4) AND ($5='' OR window_kind=$5)
   ORDER BY window_end DESC, definition_version DESC, source, metric_reading_id DESC LIMIT $6
  ) x`, p.metric, p.scopeKind, p.scopeRef, p.label, p.windowKind, p.limit).Scan(&doc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	var out struct {
		Readings []json.RawMessage `json:"readings"`
	}
	if err = json.Unmarshal(doc, &out); err == nil {
		for i := range out.Readings {
			out.Readings[i], err = enforceMetricFloor(out.Readings[i])
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

type comparisonReading struct {
	Version    int       `json:"definition_version"`
	SHA        string    `json:"definition_sha256"`
	Unit       string    `json:"unit"`
	Start      time.Time `json:"window_start"`
	End        time.Time `json:"window_end"`
	Value      *float64  `json:"value"`
	Conclusive bool      `json:"conclusive"`
}

func metricChange(b, c comparisonReading) (bool, bool, *float64) {
	same := b.Version == c.Version && b.SHA == c.SHA
	conclusive := same && b.Unit == c.Unit && b.Conclusive && c.Conclusive && b.Value != nil && c.Value != nil
	if !conclusive {
		return same, false, nil
	}
	change := *c.Value - *b.Value
	return same, true, &change
}

func (a *API) metricCompare(w http.ResponseWriter, r *http.Request) {
	p, err := parseMetricQuery(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	q := r.URL.Query()
	baseline := q.Get("baseline")
	if baseline != "" && !strings.HasPrefix(baseline, "baseline:") {
		writeError(w, 400, "bad_request", "baseline must name a baseline label")
		return
	}
	var from, to time.Time
	if p.label != "" {
		if !strings.HasPrefix(p.label, "group:") || q.Get("from") != "" || q.Get("to") != "" {
			writeError(w, 400, "bad_request", "use a group label or from and to")
			return
		}
	} else {
		from, err = time.Parse(time.RFC3339Nano, q.Get("from"))
		if err == nil {
			to, err = time.Parse(time.RFC3339Nano, q.Get("to"))
		}
		if err != nil || !to.After(from) {
			writeError(w, 400, "bad_request", "from and to must define a positive RFC3339 window")
			return
		}
	}
	if p.scopeKind == "" {
		p.scopeKind = "all"
	}
	var bdoc, cdoc []byte
	err = store.InTx(r.Context(), a.DB, func(tx pgx.Tx) error {
		// Read both sides in one statement, so concurrent writers cannot change
		// which baseline/current pair is selected between the two reads.
		return tx.QueryRow(r.Context(), `WITH baseline AS (
   SELECT `+metricProjection+` FROM agentrt.metric_reading
   WHERE metric_id=$1 AND scope_kind=$2 AND scope_ref=$3
    AND (($4='' AND label LIKE 'baseline:%') OR label=$4)
   ORDER BY window_end DESC, definition_version DESC, computed_at DESC, metric_reading_id DESC LIMIT 1
  ), current AS (
   SELECT `+metricProjection+` FROM agentrt.metric_reading
   WHERE metric_id=$1 AND scope_kind=$2 AND scope_ref=$3
    AND (($5<>'' AND label=$5) OR ($5='' AND label='' AND window_start=$6 AND window_end=$7))
    AND window_end-window_start=(SELECT window_end-window_start FROM baseline)
   ORDER BY definition_version DESC, computed_at DESC, metric_reading_id DESC LIMIT 1
  ) SELECT (SELECT to_jsonb(b) FROM baseline b), (SELECT to_jsonb(c) FROM current c)`,
			p.metric, p.scopeKind, p.scopeRef, baseline, p.label, from, to).Scan(&bdoc, &cdoc)
	})
	if err != nil {
		a.fail(w, err)
		return
	}
	if len(bdoc) == 0 || len(cdoc) == 0 {
		writeError(w, 404, "not_found", "baseline or current reading of the same length is missing")
		return
	}
	bdoc, err = enforceMetricFloor(bdoc)
	if err == nil {
		cdoc, err = enforceMetricFloor(cdoc)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	var b, c comparisonReading
	if err = json.Unmarshal(bdoc, &b); err == nil {
		err = json.Unmarshal(cdoc, &c)
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	same, conclusive, change := metricChange(b, c)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"metric_id": p.metric, "baseline": json.RawMessage(bdoc), "current": json.RawMessage(cdoc), "same_definition": same, "conclusive": conclusive, "change": change})
}
