package network

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// probeTracker is process-local L0 streak state for anomaly / predictive-lite.
type probeTracker struct {
	mu           sync.Mutex
	failStreak   int
	lastOK       time.Time
	lastFail     time.Time
	downEvents   map[string][]time.Time // device mac/name → recent down observations
}

func newProbeTracker() *probeTracker {
	return &probeTracker{downEvents: map[string][]time.Time{}}
}

func (t *probeTracker) recordOK() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failStreak = 0
	t.lastOK = time.Now().UTC()
}

func (t *probeTracker) recordFail() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failStreak++
	t.lastFail = time.Now().UTC()
	return t.failStreak
}

func (t *probeTracker) streak() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.failStreak
}

func (t *probeTracker) noteDeviceDown(key string, now time.Time, window time.Duration) int {
	t.mu.Lock()
	defer t.mu.Unlock()
	cut := now.Add(-window)
	events := t.downEvents[key]
	kept := events[:0]
	for _, ts := range events {
		if ts.After(cut) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	t.downEvents[key] = kept
	return len(kept)
}

// anomalyRuleConfig is env-tunable; defaults keep L0 read-first monitoring.
type anomalyRuleConfig struct {
	deviceDown         bool
	probeFailStreakMin int
	clientDropEnabled  bool
	clientDropFloor    int // alert when clients below this (0 = off)
	flapWindow         time.Duration
	flapThreshold      int
}

func loadAnomalyRules() anomalyRuleConfig {
	cfg := anomalyRuleConfig{
		deviceDown:         true,
		probeFailStreakMin: 3,
		clientDropEnabled:  false,
		clientDropFloor:    0,
		flapWindow:         30 * time.Minute,
		flapThreshold:      3,
	}
	if v := strings.TrimSpace(os.Getenv("NETWORK_ANOMALY_PROBE_STREAK")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.probeFailStreakMin = n
		}
	}
	if v := strings.TrimSpace(os.Getenv("NETWORK_ANOMALY_CLIENT_FLOOR")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.clientDropEnabled = true
			cfg.clientDropFloor = n
		}
	}
	return cfg
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}

func asInt(v any) *int {
	switch t := v.(type) {
	case float64:
		n := int(t)
		return &n
	case int:
		return &t
	default:
		return nil
	}
}

func asFloat(v any) *float64 {
	switch t := v.(type) {
	case float64:
		return &t
	case int:
		f := float64(t)
		return &f
	default:
		return nil
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func deviceStateLabel(state *int) string {
	if state == nil {
		return "unknown"
	}
	switch *state {
	case 1:
		return "online"
	case 0:
		return "offline"
	default:
		return fmt.Sprintf("state_%d", *state)
	}
}

func projectDevice(raw map[string]any) map[string]any {
	state := asInt(raw["state"])
	out := map[string]any{
		"name":        asString(raw["name"]),
		"model":       asString(raw["model"]),
		"type":        asString(raw["type"]),
		"ip":          asString(raw["ip"]),
		"mac":         asString(raw["mac"]),
		"version":     asString(raw["version"]),
		"adopted":     asBool(raw["adopted"]),
		"state":       state,
		"state_label": deviceStateLabel(state),
	}
	if u := asInt(raw["uptime"]); u != nil {
		out["uptime"] = *u
	}
	if rx := asFloat(raw["rx_bytes"]); rx != nil {
		out["rx_bytes"] = *rx
	}
	if tx := asFloat(raw["tx_bytes"]); tx != nil {
		out["tx_bytes"] = *tx
	}
	// UniFi may expose instantaneous rates under several keys.
	for _, key := range []string{"rx_rate", "rx_bytes-r", "rx_rate-r"} {
		if v := asFloat(raw[key]); v != nil {
			out["rx_rate"] = *v
			break
		}
	}
	for _, key := range []string{"tx_rate", "tx_bytes-r", "tx_rate-r"} {
		if v := asFloat(raw[key]); v != nil {
			out["tx_rate"] = *v
			break
		}
	}
	return out
}

func projectClient(raw map[string]any) map[string]any {
	out := map[string]any{
		"hostname":  asString(raw["hostname"]),
		"name":      asString(raw["name"]),
		"ip":        asString(raw["ip"]),
		"mac":       asString(raw["mac"]),
		"network":   firstString(raw, "network", "essid", "ssid"),
		"is_wired":  asBool(raw["is_wired"]),
		"last_seen": asInt(raw["last_seen"]),
	}
	if rx := asFloat(raw["rx_bytes"]); rx != nil {
		out["rx_bytes"] = *rx
	}
	if tx := asFloat(raw["tx_bytes"]); tx != nil {
		out["tx_bytes"] = *tx
	}
	for _, key := range []string{"rx_rate", "rx_bytes-r"} {
		if v := asFloat(raw[key]); v != nil {
			out["rx_rate"] = *v
			break
		}
	}
	for _, key := range []string{"tx_rate", "tx_bytes-r"} {
		if v := asFloat(raw[key]); v != nil {
			out["tx_rate"] = *v
			break
		}
	}
	return out
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := asString(m[k]); s != "" {
			return s
		}
	}
	return ""
}

func (s *Service) ensureTracker() *probeTracker {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.tracker == nil {
		s.tracker = newProbeTracker()
	}
	return s.tracker
}

// Health wraps UniFi stat/health and summarizes device online fraction.
func (s *Service) Health(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		s.ensureTracker().recordFail()
		return nil, err
	}
	rawHealth, err := client.Health(ctx)
	if err != nil {
		s.ensureTracker().recordFail()
		return nil, err
	}
	subsystems, err := parseJSONArray(rawHealth)
	if err != nil {
		s.ensureTracker().recordFail()
		return nil, err
	}

	rawDev, err := client.ListDevices(ctx)
	if err != nil {
		s.ensureTracker().recordFail()
		return nil, err
	}
	devices, err := parseJSONArray(rawDev)
	if err != nil {
		return nil, err
	}
	projected := make([]map[string]any, 0, len(devices))
	up := 0
	for _, d := range devices {
		p := projectDevice(d)
		projected = append(projected, p)
		if label, _ := p["state_label"].(string); label == "online" {
			up++
		}
	}
	total := len(projected)
	frac := 0.0
	if total > 0 {
		frac = float64(up) / float64(total)
	}
	s.ensureTracker().recordOK()

	return map[string]any{
		"reachable":           true,
		"autonomy":            "L0",
		"subsystems":          subsystems,
		"devices_up":          up,
		"devices_total":       total,
		"devices_up_fraction": round3(frac),
		"devices":             projected,
		"probe_fail_streak":   s.ensureTracker().streak(),
		"summary":             fmt.Sprintf("%d/%d devices online", up, total),
	}, nil
}

// Bandwidth aggregates rx/tx counters and rates from devices + clients.
func (s *Service) Bandwidth(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	rawDev, err := client.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := parseJSONArray(rawDev)
	if err != nil {
		return nil, err
	}
	rawSta, err := client.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	clients, err := parseJSONArray(rawSta)
	if err != nil {
		return nil, err
	}

	devOut := make([]map[string]any, 0, len(devices))
	var totRx, totTx float64
	for _, d := range devices {
		p := projectDevice(d)
		row := map[string]any{
			"name": p["name"],
			"mac":  p["mac"],
			"type": p["type"],
		}
		if v, ok := p["rx_bytes"]; ok {
			row["rx_bytes"] = v
			totRx += v.(float64)
		}
		if v, ok := p["tx_bytes"]; ok {
			row["tx_bytes"] = v
			totTx += v.(float64)
		}
		if v, ok := p["rx_rate"]; ok {
			row["rx_rate"] = v
		}
		if v, ok := p["tx_rate"]; ok {
			row["tx_rate"] = v
		}
		devOut = append(devOut, row)
	}

	cliOut := make([]map[string]any, 0, len(clients))
	for _, c := range clients {
		p := projectClient(c)
		row := map[string]any{
			"hostname": p["hostname"],
			"name":     p["name"],
			"mac":      p["mac"],
			"ip":       p["ip"],
		}
		if v, ok := p["rx_bytes"]; ok {
			row["rx_bytes"] = v
		}
		if v, ok := p["tx_bytes"]; ok {
			row["tx_bytes"] = v
		}
		if v, ok := p["rx_rate"]; ok {
			row["rx_rate"] = v
		}
		if v, ok := p["tx_rate"]; ok {
			row["tx_rate"] = v
		}
		cliOut = append(cliOut, row)
	}

	return map[string]any{
		"autonomy": "L0",
		"devices":  devOut,
		"clients":  cliOut,
		"totals": map[string]any{
			"rx_bytes": totRx,
			"tx_bytes": totTx,
		},
		"device_count": len(devOut),
		"client_count": len(cliOut),
	}, nil
}

// Anomalies evaluates config-driven L0 rules (no actuation).
func (s *Service) Anomalies(ctx context.Context) (map[string]any, error) {
	rules := loadAnomalyRules()
	tracker := s.ensureTracker()
	alerts := make([]map[string]any, 0)

	client, err := s.session(ctx)
	if err != nil {
		streak := tracker.recordFail()
		if rules.probeFailStreakMin > 0 && streak >= rules.probeFailStreakMin {
			alerts = append(alerts, map[string]any{
				"rule":     "probe_fail_streak",
				"severity": "warning",
				"message":  fmt.Sprintf("UniFi probe fail streak %d (threshold %d)", streak, rules.probeFailStreakMin),
				"streak":   streak,
			})
		}
		return map[string]any{
			"autonomy": "L0",
			"count":    len(alerts),
			"alerts":   alerts,
			"rules": map[string]any{
				"device_down":           rules.deviceDown,
				"probe_fail_streak_min": rules.probeFailStreakMin,
				"client_drop_enabled":   rules.clientDropEnabled,
				"client_drop_floor":     rules.clientDropFloor,
				"flap_threshold":        rules.flapThreshold,
			},
			"probe_ok": false,
		}, nil
	}

	rawDev, err := client.ListDevices(ctx)
	if err != nil {
		streak := tracker.recordFail()
		if rules.probeFailStreakMin > 0 && streak >= rules.probeFailStreakMin {
			alerts = append(alerts, map[string]any{
				"rule":     "probe_fail_streak",
				"severity": "warning",
				"message":  fmt.Sprintf("Device list fail streak %d", streak),
				"streak":   streak,
			})
		}
		return map[string]any{
			"autonomy": "L0",
			"count":    len(alerts),
			"alerts":   alerts,
			"probe_ok": false,
		}, nil
	}
	devices, err := parseJSONArray(rawDev)
	if err != nil {
		return nil, err
	}
	tracker.recordOK()
	now := time.Now().UTC()
	tips := make([]string, 0)

	if rules.deviceDown {
		for _, d := range devices {
			p := projectDevice(d)
			if label, _ := p["state_label"].(string); label == "offline" {
				name := asString(p["name"])
				if name == "" {
					name = asString(p["mac"])
				}
				key := asString(p["mac"])
				if key == "" {
					key = name
				}
				n := tracker.noteDeviceDown(key, now, rules.flapWindow)
				alerts = append(alerts, map[string]any{
					"rule":     "device_down",
					"severity": "warning",
					"message":  fmt.Sprintf("Device offline: %s", name),
					"device":   name,
					"mac":      p["mac"],
				})
				if n >= rules.flapThreshold {
					tip := fmt.Sprintf("%s flapping (%d downs in %s)", name, n, rules.flapWindow)
					tips = append(tips, tip)
					alerts = append(alerts, map[string]any{
						"rule":     "device_flapping",
						"severity": "info",
						"message":  tip,
						"device":   name,
					})
				}
			}
		}
	}

	if streak := tracker.streak(); rules.probeFailStreakMin > 0 && streak >= rules.probeFailStreakMin {
		alerts = append(alerts, map[string]any{
			"rule":     "probe_fail_streak",
			"severity": "warning",
			"message":  fmt.Sprintf("Probe fail streak %d", streak),
			"streak":   streak,
		})
		tips = append(tips, fmt.Sprintf("Repeated probe failures (streak %d)", streak))
	}

	if rules.clientDropEnabled {
		rawSta, err := client.ListClients(ctx)
		if err == nil {
			clients, cerr := parseJSONArray(rawSta)
			if cerr == nil && len(clients) < rules.clientDropFloor {
				alerts = append(alerts, map[string]any{
					"rule":     "client_drop",
					"severity": "warning",
					"message":  fmt.Sprintf("Client count %d below floor %d", len(clients), rules.clientDropFloor),
					"count":    len(clients),
					"floor":    rules.clientDropFloor,
				})
			}
		}
	}

	return map[string]any{
		"autonomy": "L0",
		"count":    len(alerts),
		"alerts":   alerts,
		"tips":     tips,
		"probe_ok": true,
		"rules": map[string]any{
			"device_down":           rules.deviceDown,
			"probe_fail_streak_min": rules.probeFailStreakMin,
			"client_drop_enabled":   rules.clientDropEnabled,
			"client_drop_floor":     rules.clientDropFloor,
			"flap_threshold":        rules.flapThreshold,
		},
	}, nil
}

// SLA returns a thin vertical-slice SLA summary for Console.
func (s *Service) SLA(ctx context.Context) (map[string]any, error) {
	health, err := s.Health(ctx)
	probeOK := err == nil
	anomalies, aerr := s.Anomalies(ctx)
	tips := []string{}
	if aerr == nil {
		if t, ok := anomalies["tips"].([]string); ok {
			tips = t
		} else if raw, ok := anomalies["tips"].([]any); ok {
			for _, v := range raw {
				if s, ok := v.(string); ok {
					tips = append(tips, s)
				}
			}
		}
	}

	out := map[string]any{
		"autonomy":  "L0",
		"probe_ok":  probeOK,
		"source":    "network_probe",
		"tips":      tips,
		"summary":   "Probe unavailable",
	}
	if probeOK {
		frac, _ := health["devices_up_fraction"].(float64)
		up, _ := health["devices_up"].(int)
		total, _ := health["devices_total"].(int)
		out["devices_up"] = up
		out["devices_total"] = total
		out["devices_up_fraction"] = frac
		out["probe_fail_streak"] = health["probe_fail_streak"]
		out["summary"] = fmt.Sprintf("Probe OK · %d/%d devices online (%.0f%%)", up, total, frac*100)
		if !probeOK {
			out["summary"] = "Probe failed"
		}
	} else {
		out["error"] = err.Error()
		out["probe_fail_streak"] = s.ensureTracker().streak()
		out["summary"] = fmt.Sprintf("Probe failed · streak %d", s.ensureTracker().streak())
		if len(tips) == 0 {
			tips = append(tips, "Repeated UniFi probe failures — check UCG reachability / credentials")
			out["tips"] = tips
		}
	}
	return out, nil
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
