package checklist

import (
	"context"
	"log/slog"
	"strings"
)

// What the prober watches for the bus. A flip is said once: the last signal
// said is kept with the checklist's state, so a restart neither repeats one nor
// loses one, and unknown (the prober could not read) is not a state to flip to.

// WatchKind says which event a watched thing's flips become.
type WatchKind string

const (
	WatchEnv    WatchKind = "env"
	WatchPlugin WatchKind = "plugin"
)

// Watched is one thing the prober read this run. Subject is <env>/<service>
// for WatchEnv and the plugin's name for WatchPlugin.
type Watched struct {
	Kind    WatchKind
	Subject string
	Signal  string
	Detail  string
}

func (w Watched) key() string { return string(w.Kind) + "|" + w.Subject }

// FlipFunc says a flip. An error leaves the flip to be said on the next run.
type FlipFunc func(ctx context.Context, w Watched) error

type flipFuncBox struct{ f FlipFunc }

// OnFlip sets who says the flips. Until it is set nothing is recorded, so the
// first runs of a starting process lose nothing.
func (h *Handler) OnFlip(f FlipFunc) { h.onFlip.Store(&flipFuncBox{f}) }

// sayFlips says what changed since the last signal said and records it.
func (h *Handler) sayFlips(ctx context.Context, seen []Watched) {
	box := h.onFlip.Load()
	if box == nil {
		return
	}
	flips, quiet, err := h.store.watchedChanges(seen)
	if err != nil {
		slog.Warn("checklist prober: flips not read", "err", err)
		return
	}
	said := quiet
	for _, w := range flips {
		if err := box.f(ctx, w); err != nil {
			slog.Warn("checklist prober: flip not said", "subject", w.Subject, "signal", w.Signal, "err", err)
			continue
		}
		said = append(said, w)
	}
	if err := h.store.recordWatched(said); err != nil {
		slog.Warn("checklist prober: flips not recorded", "err", err)
	}
}

// watchedChanges splits seen into flips to say and first sightings of a healthy
// thing, which are only recorded. A thing first seen unhealthy is a flip.
func (s *Store) watchedChanges(seen []Watched) (flips, quiet []Watched, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return nil, nil, err
	}
	for _, w := range seen {
		if w.Signal == SignalUnknown {
			continue
		}
		last, known := rec.Watched[w.key()]
		switch {
		case known && last == w.Signal:
		case !known && w.Signal == SignalOK:
			quiet = append(quiet, w)
		default:
			flips = append(flips, w)
		}
	}
	return flips, quiet, nil
}

func (s *Store) recordWatched(said []Watched) error {
	if len(said) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, err := s.loadLocked()
	if err != nil {
		return err
	}
	if rec.Watched == nil {
		rec.Watched = map[string]string{}
	}
	for _, w := range said {
		rec.Watched[w.key()] = w.Signal
	}
	return s.saveLocked(rec)
}

// pluginItems are checklist items a plugin's own status already says on the bus.
var pluginItems = map[string]bool{"massive-polygon": true, "ib-feed": true}

// watchedItems are this run's checklist items as things to watch.
func (p *Prober) watchedItems(sigs []ItemSignal) []Watched {
	out := make([]Watched, 0, len(sigs))
	for _, s := range sigs {
		if !pluginItems[s.ItemID] {
			out = append(out, Watched{WatchEnv, p.Env + "/" + s.ItemID, s.Signal, s.Detail})
		}
	}
	return out
}

// watchedExtras are the health reads no checklist item covers: the three
// plugins, the out-of-band operator plane and Trade's bus (read only). The
// workstation's git and probe bridges in /agent/bridge are left out: they are
// unreachable from the cluster whenever that Mac is away, which is no fault.
func (p *Prober) watchedExtras(ctx context.Context) []Watched {
	var out []Watched
	for _, plugin := range []string{"market-data", "ib-gateway", "flex-query"} {
		var st struct {
			Reachability string `json:"reachability"`
			Summary      string `json:"summary"`
		}
		if err := p.getJSON(ctx, "/api/v1/plugins/"+plugin+"/status", &st); err != nil {
			continue
		}
		out = append(out, Watched{WatchPlugin, plugin, reachSignal(st.Reachability), st.Summary})
	}
	var bridge struct {
		Runner struct {
			Status string `json:"status"`
			Error  string `json:"error"`
		} `json:"remediation_runner"`
	}
	if err := p.getJSON(ctx, "/api/v1/agent/bridge", &bridge); err == nil && bridge.Runner.Status != "" {
		sig := SignalFail
		if strings.EqualFold(bridge.Runner.Status, "ok") {
			sig = SignalOK
		}
		out = append(out, Watched{WatchEnv, p.Env + "/operator-plane", sig, bridge.Runner.Error})
	}
	var deep struct {
		Buses []struct {
			Environment  string `json:"environment"`
			Reachability string `json:"reachability"`
			Monitor      struct {
				Detail string `json:"detail"`
			} `json:"monitor"`
		} `json:"buses"`
	}
	if err := p.getJSON(ctx, "/api/v1/satellite/bus-deep", &deep); err == nil {
		for _, b := range deep.Buses {
			if b.Environment == p.Env {
				out = append(out, Watched{WatchEnv, p.Env + "/trade-bus", reachSignal(b.Reachability), b.Monitor.Detail})
			}
		}
	}
	return out
}
