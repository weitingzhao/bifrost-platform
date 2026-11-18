package server

import (
	"context"
	"fmt"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/readerseen"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	bushook "github.com/weitingzhao/bifrost-platform/api/internal/bus/hook"
	"github.com/weitingzhao/bifrost-platform/api/internal/bus/nextgen"
	"github.com/weitingzhao/bifrost-platform/api/internal/delivery"
)

// GET /api/v1/bus/nextgen: the communication layer's change of generation,
// from its registers (bus/nextgen): how many old channels are left of 153
// and of the target 64, by disposition, the latest retired; the readers of
// the source switch; the baselines, each value queried from Prometheus at
// the time it names. The registers' tests hold them to the code.

type nextgenReaderGroup struct {
	nextgen.ReaderGroup
	Counts map[string]int `json:"counts"`
}

type nextgenBaseline struct {
	nextgen.Baseline
	Value      *float64 `json:"value,omitempty"`
	Unmeasured string   `json:"unmeasured,omitempty"`
}

type busNextgen struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Progress    nextgen.Progress     `json:"progress"`
	Readers     []nextgenReaderGroup `json:"readers"`
	Baselines   []nextgenBaseline    `json:"baselines"`
}

func (s *Server) handleBusNextgen(w http.ResponseWriter, r *http.Request) {
	chs, err := nextgen.Channels()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	out := busNextgen{GeneratedAt: time.Now().UTC(), Progress: nextgen.Summarize(chs)}
	groups, err := nextgen.Readers()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	evidence := s.readerEvidence(r.Context(), out.GeneratedAt)
	nextgen.ApplyEvidence(&out.Progress, chs, evidence)
	for _, g := range groups {
		ng := nextgenReaderGroup{ReaderGroup: g, Counts: map[string]int{}}
		for i, rd := range g.Readers {
			ng.Counts[rd.Status]++
			ng.Readers[i].Note = watchNote(rd)
			if rd.Status == nextgen.StatusMoved {
				e := evidence(rd.ID)
				ng.Readers[i].Evidence = &e
				ng.Counts["evidence_"+e.State]++
			}
		}
		out.Readers = append(out.Readers, ng)
	}
	bs, err := nextgen.Baselines()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, b := range bs {
		nb := nextgenBaseline{Baseline: b}
		if v, err := s.baselineValue(r.Context(), b); err != nil {
			nb.Unmeasured = err.Error()
		} else {
			nb.Value = &v
		}
		out.Baselines = append(out.Baselines, nb)
	}
	writeJSON(w, http.StatusOK, out)
}

// baselineValue queries a baseline at its time.
func (s *Server) baselineValue(ctx context.Context, b nextgen.Baseline) (float64, error) {
	at, err := time.Parse(time.RFC3339, b.At)
	if err != nil {
		return 0, fmt.Errorf("%s: time %q", b.Key, b.At)
	}
	v, err := s.promql(ctx, strings.ReplaceAll(b.Query, "{{at}}", fmt.Sprint(at.Unix())))
	if err != nil {
		return 0, err
	}
	x, ok := v[""]
	if !ok {
		return 0, fmt.Errorf("Prometheus has no value at %s", b.At)
	}
	return x, nil
}

// baselineInto sets a row's Before from the baseline of that key, when the
// system has one and can read it.
func (s *Server) baselineInto(ctx context.Context, row *effectRow, key string) {
	bs, err := nextgen.Baselines()
	if err != nil {
		return
	}
	for _, b := range bs {
		if b.Key != key {
			continue
		}
		if v, err := s.baselineValue(ctx, b); err == nil {
			row.Before = fmt.Sprintf("%.0f per 10 min at %s (%s)", v, b.At, b.Note)
			row.BeforeKind = "baseline"
		}
		return
	}
}

// readerEvidence reads, from this environment's metrics, whether each moved
// reader really answers from its new source (bus/readerseen): the answer for
// one reader id, or for "reader:<id>" and "topic:<type>" (the relay publishing
// that event). Prometheus not answering is "unmeasured", not a failure of
// the reader.
func (s *Server) readerEvidence(ctx context.Context, now time.Time) func(string) nextgen.Evidence {
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	ns := "bifrost-platform-" + env
	window := fmt.Sprintf("%dd", int(nextgen.EvidenceWindow.Hours()/24))
	read := func(q string, metric string) (map[string]float64, error) {
		return s.promql(ctx, fmt.Sprintf(q, metric, ns, window))
	}
	const lastQ, runsQ = `max by (reader) (max_over_time(%s{namespace=%q}[%s]))`, `sum by (reader) (increase(%s{namespace=%q}[%s]))`
	const lastT, runsT = `max by (type) (max_over_time(%s{namespace=%q}[%s]))`, `sum by (type) (increase(%s{namespace=%q}[%s]))`
	var last, runs, topicLast, topicRuns map[string]float64
	var err error
	for _, q := range []struct {
		to            *map[string]float64
		query, metric string
	}{
		{&last, lastQ, readerseen.MetricLast}, {&runs, runsQ, readerseen.MetricRuns},
		{&topicLast, lastT, "bifrost_bus_relay_last_published_timestamp_seconds"}, {&topicRuns, runsT, "bifrost_bus_relay_published_by_type_total"},
	} {
		if *q.to, err = read(q.query, q.metric); err != nil {
			slog.Info("bus readers: run evidence not read", "error", err)
			return func(string) nextgen.Evidence { return nextgen.Evidence{State: nextgen.EvidenceUnmeasured} }
		}
	}
	return func(id string) nextgen.Evidence {
		if t, ok := strings.CutPrefix(id, "topic:"); ok {
			return nextgen.JudgeEvidence(now, topicLast[t], topicRuns[t])
		}
		r := strings.TrimPrefix(id, "reader:")
		return nextgen.JudgeEvidence(now, last[r], runs[r])
	}
}

// evidenceWhen says a moved reader's evidence in a line.
func evidenceWhen(e nextgen.Evidence, now time.Time) string {
	switch e.State {
	case nextgen.EvidenceRunning:
		return "ran " + now.Sub(*e.LastSeen).Round(time.Second).String() + " ago"
	case nextgen.EvidenceStale:
		return "NO RECENT RUN: last " + now.Sub(*e.LastSeen).Round(time.Minute).String() + " ago"
	case nextgen.EvidenceNone:
		return "NO RUN EVIDENCE in 7 days"
	}
	return "run evidence not measured"
}

// effectSwitchRows are the source switch's two groups, counted from the
// register (bus/nextgen readers.yaml), each reader an item; a moved reader
// says when it last ran on its new source.
func effectSwitchRows(evidence func(string) nextgen.Evidence, now time.Time) []effectRow {
	groups, err := nextgen.Readers()
	if err != nil {
		return []effectRow{{Key: "switch", Label: "Source readers moved", Unmeasured: err.Error()}}
	}
	var rows []effectRow
	for _, g := range groups {
		counts := map[string]int{}
		running := 0
		var items []effectItem
		for _, rd := range g.Readers {
			counts[rd.Status]++
			when := rd.Status
			if rd.At != "" {
				when += " " + rd.At
			}
			if rd.Status == nextgen.StatusMoved {
				e := evidence(rd.ID)
				if e.State == nextgen.EvidenceRunning {
					running++
				}
				when += " · " + evidenceWhen(e, now)
			}
			ref := watchNote(rd)
			if rd.Owner != "" {
				ref = strings.TrimSpace(rd.Owner + ": " + ref)
			}
			items = append(items, effectItem{What: rd.What, When: when, Ref: ref})
		}
		rows = append(rows, effectRow{
			Key:   "switch_" + g.ID,
			Label: "Source switch: " + g.Title,
			Value: fmt.Sprintf("%d moved (%d seen running), %d retired, %d kept by design, %d open (of %d)",
				counts[nextgen.StatusMoved], running, counts[nextgen.StatusRetired], counts[nextgen.StatusKept], counts[nextgen.StatusOpen], len(g.Readers)),
			How:   "Counted from the register bus/nextgen/readers.yaml; its test checks each moved reader's code is there and each retired one's is gone. Seen running: the reader answered from its new source in this environment, as recently as its own pace says (bifrost_bus_reader_new_source_*, last 7 days).",
			Items: items,
		})
	}
	return rows
}

// earnedReadRoutes are the display routes whose own readings show they have
// earned the run cache's trust in this environment: over the last 7 days at
// least N clean comparisons with the list and not one difference
// (bifrost_bus_readers_compared/mismatch_total, delivery's side by side).
// Prometheus not answering earns nothing: the routes compare again.
func (s *Server) earnedReadRoutes(ctx context.Context) map[string]bool {
	env := strings.TrimSpace(os.Getenv("AGENTRT_ENV"))
	if env == "" || s.telemetry == nil {
		return nil
	}
	ns := "bifrost-platform-" + env
	compared, err := s.promql(ctx, fmt.Sprintf(`sum by (route) (increase(bifrost_bus_readers_compared_total{namespace=%q}[7d]))`, ns))
	if err != nil {
		slog.Info("bus readers: earned trust not read; every route compares", "error", err)
		return nil
	}
	mismatch, err := s.promql(ctx, fmt.Sprintf(`sum by (route) (increase(bifrost_bus_readers_mismatch_total{namespace=%q}[7d]))`, ns))
	if err != nil {
		slog.Info("bus readers: earned trust not read; every route compares", "error", err)
		return nil
	}
	out := nextgen.EarnedRoutes(delivery.CacheRoutes(), compared, mismatch, delivery.ReadersCompareN())
	slog.Info("bus readers: trust from the readings", "earned", out, "compared", compared, "mismatch", mismatch)
	return out
}

// watchNote is a reader's note, and for one waiting on a repository the
// ledger watches, whether gate-git watches it now (BUS_GIT_REPOS), read
// when asked.
func watchNote(rd nextgen.Reader) string {
	if rd.WatchRepo == "" {
		return rd.Note
	}
	state := "not watched yet"
	if slices.Contains(bushook.ReposFromEnv(), rd.WatchRepo) {
		state = "watched"
	}
	return strings.TrimSpace(fmt.Sprintf("%s (%s by gate-git: BUS_GIT_REPOS=%s)", rd.Note, rd.WatchRepo+" "+state, strings.Join(bushook.ReposFromEnv(), ",")))
}
