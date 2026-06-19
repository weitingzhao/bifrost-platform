package promote

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/probe"
)

var tierBManualItems = []struct {
	id    string
	label string
}{
	{id: "ib-tws-live", label: "IB TWS live connection verified (manual)"},
	{id: "massive-ws-quotes", label: "Massive WS receiving option quotes (manual)"},
	{id: "celery-workers", label: "Celery workers processing tasks (manual)"},
}

func (s *Service) TierBStatus(ctx context.Context) TierBStatusResponse {
	now := time.Now().UTC()
	out := TierBStatusResponse{
		Items:       []TierBItemView{},
		GeneratedAt: now,
		Detail:      "Tier B acceptance — automated probes + Owner manual sign-off",
	}
	signoff, _ := s.store.LoadTierB()
	if signoff != nil {
		out.SignedOff = true
		at := signoff.At.UTC()
		out.SignoffAt = &at
		out.SignedBy = signoff.SignedBy
		out.Notes = signoff.Notes
	}

	for _, p := range s.tierBAutoProbes() {
		item := s.probeTierBHTTP(ctx, p.id, p.label, p.url)
		out.Items = append(out.Items, item)
	}
	for _, m := range tierBManualItems {
		reach := probe.ReachUnknown
		detail := "Owner sign-off required"
		if out.SignedOff {
			reach = probe.ReachOK
			detail = "Covered by Tier B sign-off"
		}
		out.Items = append(out.Items, TierBItemView{
			ID: m.id, Label: m.label, Kind: "manual", Required: true,
			Reachability: reach, Detail: detail,
		})
	}

	autoFail := false
	autoUnknown := false
	for _, it := range out.Items {
		if it.Kind != "auto" || !it.Required {
			continue
		}
		if it.Reachability == probe.ReachFail {
			autoFail = true
		}
		if it.Reachability == probe.ReachUnknown {
			autoUnknown = true
		}
	}

	out.Ready = out.SignedOff && !autoFail && !autoUnknown
	switch {
	case out.Ready:
		out.Reachability = probe.ReachOK
		out.Detail = "Tier B signed off — STG extended acceptance complete"
	case autoFail:
		out.Reachability = probe.ReachFail
		out.Detail = "Tier B auto probes failing — fix before sign-off"
	case out.SignedOff:
		out.Reachability = probe.ReachDegraded
		out.Detail = "Signed off but some auto probes not green"
	default:
		out.Reachability = probe.ReachDegraded
		out.Detail = "Complete manual IB/Massive checks then admin sign-off"
	}
	return out
}

func (s *Service) SignTierB(ctx context.Context, notes, signedBy string) (TierBSignoffResponse, error) {
	now := time.Now().UTC()
	status := s.TierBStatus(ctx)
	autoFail := false
	for _, it := range status.Items {
		if it.Kind == "auto" && it.Required && it.Reachability == probe.ReachFail {
			autoFail = true
			break
		}
	}
	if autoFail {
		return TierBSignoffResponse{}, fmt.Errorf("tier B auto probes failing — resolve before sign-off")
	}
	rec := TierBSignoffRecord{At: now, SignedBy: signedBy, Notes: strings.TrimSpace(notes)}
	if err := s.store.SaveTierB(rec); err != nil {
		return TierBSignoffResponse{}, err
	}
	_ = s.store.AppendLog(fmt.Sprintf("tier-b signoff by %s", signedBy))
	status = s.TierBStatus(ctx)
	return TierBSignoffResponse{
		OK: true, Action: "promote.tier-b-signoff", Target: "tier-b",
		Changed: true, Message: "Tier B sign-off recorded",
		Status: status, GeneratedAt: now,
	}, nil
}

func (s *Service) tierBAutoProbes() []struct {
	id, label, url string
} {
	gw := ""
	if s.cfg != nil {
		if entry := s.cfg.DefaultCluster(); entry != nil {
			gw = strings.TrimRight(entry.ResolvedStgGatewayURL(), "/")
		}
	}
	if gw == "" {
		return nil
	}
	return []struct{ id, label, url string }{
		{id: "tierb-daemon", label: "Daemon status (monitor API)", url: gw + "/api/monitor/status"},
		{id: "tierb-ops", label: "Ops API health", url: gw + "/api/ops/status"},
		{id: "tierb-socket-massive", label: "Socket ingest services (ops)", url: gw + "/api/ops/market-ingest/services"},
	}
}

func (s *Service) probeTierBHTTP(ctx context.Context, id, label, url string) TierBItemView {
	if url == "" {
		return TierBItemView{
			ID: id, Label: label, Kind: "auto", Required: true,
			Reachability: probe.ReachUnknown, Detail: "URL not configured",
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return TierBItemView{
			ID: id, Label: label, Kind: "auto", Required: true,
			Reachability: probe.ReachFail, Detail: err.Error(),
		}
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return TierBItemView{
			ID: id, Label: label, Kind: "auto", Required: true,
			Reachability: probe.ReachFail, Detail: err.Error(),
		}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	reach := probe.ReachOK
	detail := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if resp.StatusCode >= 500 {
		reach = probe.ReachFail
	} else if resp.StatusCode >= 400 {
		reach = probe.ReachDegraded
	}
	return TierBItemView{
		ID: id, Label: label, Kind: "auto", Required: true,
		Reachability: reach, Detail: detail,
	}
}
