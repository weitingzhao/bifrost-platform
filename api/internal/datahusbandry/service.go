package datahusbandry

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Snapshot — unified husbandry verdict for Console (Wave 2).
type Snapshot struct {
	GeneratedAt time.Time   `json:"generated_at"`
	Overall     string      `json:"overall"` // healthy | caution | degraded | unknown
	Detail      string      `json:"detail"`
	Lanes       []LaneView  `json:"lanes"`
	Note        string      `json:"note"`
}

type LaneView struct {
	ID      string `json:"id"` // market_batch | flex_batch | research_olap
	Label   string `json:"label"`
	Verdict string `json:"verdict"`
	Detail  string `json:"detail"`
	Source  string `json:"source,omitempty"`
}

type proxyer interface {
	Proxy(r *http.Request, path string) (*http.Response, error)
}

type Service struct {
	market   proxyer
	flex     proxyer
	research proxyer
}

func NewService(market, flex, research proxyer) *Service {
	return &Service{market: market, flex: flex, research: research}
}

func (s *Service) Snapshot(ctx context.Context) Snapshot {
	now := time.Now().UTC()
	out := Snapshot{
		GeneratedAt: now,
		Note:        "Ground truth = freshness/coverage/signal-health — not K8s Job Complete. void ≠ fail. STG Trade clone ≠ Golden Source.",
		Lanes:       make([]LaneView, 0, 3),
	}

	out.Lanes = append(out.Lanes, s.probeMarket(ctx))
	out.Lanes = append(out.Lanes, s.probeFlex(ctx))
	out.Lanes = append(out.Lanes, s.probeResearch(ctx))

	out.Overall, out.Detail = rollup(out.Lanes)
	return out
}

func rollup(lanes []LaneView) (string, string) {
	worst := "healthy"
	rank := map[string]int{"healthy": 0, "due": 1, "draining": 1, "caution": 2, "missed": 3, "degraded": 3, "unknown": 2}
	var bad []string
	for _, l := range lanes {
		if rank[l.Verdict] > rank[worst] {
			worst = l.Verdict
		}
		if l.Verdict != "healthy" && l.Verdict != "draining" && l.Verdict != "due" {
			bad = append(bad, l.ID+":"+l.Verdict)
		}
	}
	if worst == "healthy" {
		return "healthy", "all husbandry lanes ok"
	}
	if len(bad) == 0 {
		return "caution", "lanes draining or due"
	}
	detail := ""
	for i, b := range bad {
		if i > 0 {
			detail += "; "
		}
		detail += b
	}
	if worst == "unknown" {
		return "unknown", detail
	}
	return "degraded", detail
}

func (s *Service) getJSON(ctx context.Context, p proxyer, path string, dest any) error {
	if p == nil {
		return errUnavailable("proxy nil")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://platform.local"+path, nil)
	if err != nil {
		return err
	}
	resp, err := p.Proxy(req, path)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return errUnavailable(string(body))
	}
	return json.Unmarshal(body, dest)
}

type simpleError string

func (e simpleError) Error() string { return string(e) }
func errUnavailable(msg string) error { return simpleError(msg) }

func (s *Service) probeMarket(ctx context.Context) LaneView {
	lane := LaneView{ID: "market_batch", Label: "Market batch", Verdict: "unknown", Detail: "unreachable"}
	var dash struct {
		Husbandry *struct {
			Verdict string `json:"verdict"`
			Detail  string `json:"detail"`
		} `json:"husbandry"`
		Schedule *struct {
			Verdict string `json:"verdict"`
		} `json:"schedule"`
		Queue *struct {
			Verdict string `json:"verdict"`
		} `json:"queue"`
	}
	if err := s.getJSON(ctx, s.market, "/market/ingest/queue-dashboard", &dash); err != nil {
		lane.Detail = err.Error()
		return lane
	}
	if dash.Husbandry != nil && dash.Husbandry.Verdict != "" {
		lane.Verdict = dash.Husbandry.Verdict
		lane.Detail = dash.Husbandry.Detail
		return lane
	}
	if dash.Schedule != nil {
		lane.Verdict = dash.Schedule.Verdict
		lane.Detail = "schedule=" + dash.Schedule.Verdict
	}
	return lane
}

func (s *Service) probeFlex(ctx context.Context) LaneView {
	lane := LaneView{ID: "flex_batch", Label: "IB Flex", Verdict: "unknown", Detail: "unreachable"}
	var cfg struct {
		Source string `json:"source"`
		Tokens struct {
			HostTokenSet      bool `json:"host_token_set"`
			SecondaryTokenSet bool `json:"secondary_token_set"`
		} `json:"tokens"`
	}
	if err := s.getJSON(ctx, s.flex, "/flex/config/summary", &cfg); err != nil {
		lane.Detail = err.Error()
		return lane
	}
	lane.Source = cfg.Source
	if cfg.Source == "none" || (!cfg.Tokens.HostTokenSet && !cfg.Tokens.SecondaryTokenSet) {
		lane.Verdict = "degraded"
		lane.Detail = "token source=none — enqueue fail-closed"
		return lane
	}
	var fresh struct {
		Dimensions []struct {
			Dimension string `json:"dimension"`
			Status    string `json:"status"`
			Stale     bool   `json:"stale"`
		} `json:"dimensions"`
	}
	if err := s.getJSON(ctx, s.flex, "/flex/coverage/freshness", &fresh); err != nil {
		lane.Verdict = "caution"
		lane.Detail = "tokens ok · freshness unreachable: " + err.Error()
		return lane
	}
	stale := 0
	for _, d := range fresh.Dimensions {
		if d.Stale || d.Status == "stale" || d.Status == "failed" {
			stale++
		}
	}
	if stale > 0 {
		lane.Verdict = "degraded"
		lane.Detail = "freshness stale dimensions"
		return lane
	}
	lane.Verdict = "healthy"
	lane.Detail = "source=secret · freshness ok"
	return lane
}

func (s *Service) probeResearch(ctx context.Context) LaneView {
	lane := LaneView{ID: "research_olap", Label: "Research OLAP", Verdict: "unknown", Detail: "unreachable"}

	productVerdict, productDetail := s.probeResearchProduct(ctx)
	batchVerdict, batchDetail := s.probeResearchBatch(ctx)
	lane.Verdict, lane.Detail = rollupResearchOLAP(productVerdict, productDetail, batchVerdict, batchDetail)
	return lane
}

func (s *Service) probeResearchProduct(ctx context.Context) (verdict, detail string) {
	var wrap struct {
		OK   bool `json:"ok"`
		Data struct {
			Overall   string `json:"overall"`
			Freshness []struct {
				Label  string   `json:"label"`
				Status string   `json:"status"`
				AgeH   *float64 `json:"age_hours"`
			} `json:"freshness"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, s.research, "/research/signal-health", &wrap); err != nil {
		return "caution", "signal-health unavailable: " + err.Error()
	}
	rows := wrap.Data.Freshness
	if len(rows) == 0 {
		return "caution", "signal-health returned no freshness rows"
	}
	stale := 0
	for _, t := range rows {
		if t.Status == "stale" || t.Status == "missing" || t.Status == "empty" {
			stale++
		}
	}
	if wrap.Data.Overall == "degraded" || wrap.Data.Overall == "empty" || stale > 0 {
		return "degraded", "stale/missing feature tables"
	}
	return "healthy", "signal-health tables ok"
}

func (s *Service) probeResearchBatch(ctx context.Context) (verdict, detail string) {
	var wrap struct {
		OK   bool `json:"ok"`
		Data struct {
			Verdict       string `json:"verdict"`
			Detail        string `json:"detail"`
			Overdue       bool   `json:"overdue"`
			LastRunStatus string `json:"last_run_status"`
		} `json:"data"`
	}
	if err := s.getJSON(ctx, s.research, "/research/orchestration/status", &wrap); err != nil {
		return "unknown", "orchestration unprobed: " + err.Error()
	}
	v := wrap.Data.Verdict
	if v == "" {
		return "unknown", "orchestration empty verdict"
	}
	d := wrap.Data.Detail
	if d == "" {
		d = "orchestration=" + v
	}
	return v, d
}

// rollupResearchOLAP merges Product asof + Batch Dagster into research_olap.
// Feedstock (market/flex) is never folded in here.
func rollupResearchOLAP(productVerdict, productDetail, batchVerdict, batchDetail string) (string, string) {
	rank := map[string]int{
		"healthy": 0, "due": 1, "draining": 1, "caution": 2,
		"missed": 3, "degraded": 3, "unknown": 2,
	}
	worst := productVerdict
	if rank[batchVerdict] > rank[worst] {
		worst = batchVerdict
	}
	// Product ok + batch unknown → caution (orchestration unprobed)
	if productVerdict == "healthy" && (batchVerdict == "unknown" || batchVerdict == "") {
		return "caution", "product ok · " + batchDetail
	}
	detail := productDetail
	if batchDetail != "" {
		if detail != "" {
			detail += " · "
		}
		detail += batchDetail
	}
	if worst == "" {
		return "unknown", detail
	}
	return worst, detail
}
