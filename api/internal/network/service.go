package network

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

var zoneSpecs = []struct {
	ZoneName    string
	NetworkName string
}{
	{"Bifrost Server", "Server"},
	{"Bifrost Work", "Work"},
	{"Bifrost Family", "Family"},
	{"Bifrost IoT", "Home"},
	{"Bifrost Default", "Default"},
}

var expectedPolicyNames = []string{
	"Bifrost | REJECT Family → Server",
	"Bifrost | REJECT IoT → Server",
	"Bifrost | REJECT IoT → Family",
	"Bifrost | REJECT Family → IoT",
	"Bifrost | ALLOW Work → Server",
	"Bifrost | ALLOW Family → NAS Plex/SMB",
	"Bifrost | ALLOW IoT → NAS Plex",
	"Bifrost | ALLOW Server → IoT",
}

type networkRef struct {
	Name    string `json:"name"`
	MongoID string `json:"mongo_id"`
	VLAN    *int   `json:"vlan,omitempty"`
}

type Service struct {
	dial          func(ctx context.Context) (*unifi.Client, error)
	applyFirewall func(ctx context.Context, includeDefaultDeny bool) (map[string]any, error)

	// Cached UniFi session — reuse cookie across /api/v1/network/* calls to avoid
	// UniFi OS login rate limits (AUTHENTICATION_FAILED_LIMIT_REACHED / HTTP 429).
	sessionMu sync.Mutex
	client    *unifi.Client
}

type ServiceOption func(*Service)

func WithDial(fn func(ctx context.Context) (*unifi.Client, error)) ServiceOption {
	return func(s *Service) { s.dial = fn }
}

func WithApplyFirewall(fn func(ctx context.Context, includeDefaultDeny bool) (map[string]any, error)) ServiceOption {
	return func(s *Service) { s.applyFirewall = fn }
}

func NewService(opts ...ServiceOption) *Service {
	s := &Service{dial: defaultDial}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

func defaultDial(ctx context.Context) (*unifi.Client, error) {
	cfg, err := unifi.ConfigFromEnv()
	if err != nil {
		return nil, err
	}
	client := unifi.New(cfg)
	if err := client.EnsureLogin(ctx); err != nil {
		return nil, fmt.Errorf("unifi login: %w", err)
	}
	return client, nil
}

// session returns a process-cached UniFi client. Login happens once (or after
// InvalidateSession / 401 retry inside the client), not on every API request.
func (s *Service) session(ctx context.Context) (*unifi.Client, error) {
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if s.client != nil && s.client.LoggedIn() {
		return s.client, nil
	}
	client, err := s.dial(ctx)
	if err != nil {
		return nil, err
	}
	s.client = client
	return client, nil
}

func parseJSONArray(raw json.RawMessage) ([]map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return arr, nil
	}
	var wrapped struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &wrapped); err != nil {
		return nil, err
	}
	return wrapped.Data, nil
}

func loadNetworks(ctx context.Context, client *unifi.Client) (map[string]networkRef, error) {
	raw, err := client.LegacyGet(ctx, "/rest/networkconf")
	if err != nil {
		return nil, err
	}
	items, err := parseJSONArray(raw)
	if err != nil {
		return nil, err
	}
	out := map[string]networkRef{}
	for _, n := range items {
		if purpose, _ := n["purpose"].(string); purpose != "corporate" {
			continue
		}
		name, _ := n["name"].(string)
		mongoID, _ := n["_id"].(string)
		var vlan *int
		if v, ok := n["vlan"].(float64); ok {
			iv := int(v)
			vlan = &iv
		}
		out[name] = networkRef{Name: name, MongoID: mongoID, VLAN: vlan}
	}
	return out, nil
}

func findZoneForSpec(zones []map[string]any, zoneName string) map[string]any {
	for _, z := range zones {
		if name, _ := z["name"].(string); name == zoneName {
			return z
		}
	}
	if zoneName == "Bifrost Family" {
		for _, z := range zones {
			if strings.HasPrefix(fmt.Sprint(z["name"]), "Bifrost Family") {
				return z
			}
		}
	}
	return nil
}

func networkIDsEqual(got any, want string) bool {
	ids, ok := got.([]any)
	if !ok || len(ids) != 1 {
		return false
	}
	id, _ := ids[0].(string)
	return id == want
}

func (s *Service) Status(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	cfg := client.Config()
	raw, err := client.LegacyGet(ctx, "/stat/sysinfo")
	if err != nil {
		return nil, err
	}
	version, err := sysinfoVersion(raw)
	if err != nil {
		return nil, err
	}
	integrationOK := false
	if cfg.APIKey != "" {
		integrationOK, _ = client.IntegrationSitesHaveID(ctx)
	}
	return map[string]any{
		"host":                   cfg.Host,
		"site":                   cfg.Site,
		"reachable":              true,
		"controller_version":     version,
		"auth":                   "session",
		"session_user":           cfg.User,
		"integration_key_usable": integrationOK,
		"session_path":           "SESSION_v2",
	}, nil
}

func (s *Service) Zones(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := client.ListZones(ctx)
	if err != nil {
		return nil, err
	}
	zones, err := parseJSONArray(raw)
	if err != nil {
		return nil, err
	}
	bifrost := make([]map[string]any, 0)
	for _, z := range zones {
		name, _ := z["name"].(string)
		if strings.HasPrefix(name, "Bifrost") || name == "Internal" {
			bifrost = append(bifrost, z)
		}
	}
	return map[string]any{
		"count":   len(zones),
		"bifrost": bifrost,
		"zones":   zones,
	}, nil
}

func (s *Service) Policies(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := client.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := parseJSONArray(raw)
	if err != nil {
		return nil, err
	}
	bifrost := make([]map[string]any, 0)
	for _, p := range policies {
		if strings.HasPrefix(fmt.Sprint(p["name"]), "Bifrost |") {
			bifrost = append(bifrost, p)
		}
	}
	return map[string]any{
		"count":            len(policies),
		"bifrost_count":    len(bifrost),
		"bifrost_policies": bifrost,
	}, nil
}

func (s *Service) Devices(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := client.ListDevices(ctx)
	if err != nil {
		return nil, err
	}
	devices, err := parseJSONArray(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(devices), "devices": devices}, nil
}

func (s *Service) Clients(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	raw, err := client.ListClients(ctx)
	if err != nil {
		return nil, err
	}
	clients, err := parseJSONArray(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(clients), "clients": clients}, nil
}

func (s *Service) Audit(ctx context.Context) (map[string]any, error) {
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	cfg := client.Config()

	rawSys, err := client.LegacyGet(ctx, "/stat/sysinfo")
	if err != nil {
		return nil, err
	}
	version, err := sysinfoVersion(rawSys)
	if err != nil {
		return nil, err
	}

	networks, err := loadNetworks(ctx, client)
	if err != nil {
		return nil, err
	}

	rawZones, err := client.ListZones(ctx)
	if err != nil {
		return nil, err
	}
	zones, err := parseJSONArray(rawZones)
	if err != nil {
		return nil, err
	}

	rawPolicies, err := client.ListPolicies(ctx)
	if err != nil {
		return nil, err
	}
	policies, err := parseJSONArray(rawPolicies)
	if err != nil {
		return nil, err
	}

	var gaps []string
	for _, spec := range zoneSpecs {
		net, ok := networks[spec.NetworkName]
		if !ok {
			gaps = append(gaps, fmt.Sprintf("%s: network %s missing", spec.ZoneName, spec.NetworkName))
			continue
		}
		z := findZoneForSpec(zones, spec.ZoneName)
		if z == nil || !networkIDsEqual(z["network_ids"], net.MongoID) {
			gaps = append(gaps, fmt.Sprintf("%s: not bound to %s", spec.ZoneName, spec.NetworkName))
		}
	}

	existing := map[string]bool{}
	for _, p := range policies {
		name, _ := p["name"].(string)
		existing[name] = true
	}
	var missingPolicies []string
	for _, name := range expectedPolicyNames {
		if !existing[name] {
			missingPolicies = append(missingPolicies, name)
		}
	}

	classification := "POLICY_NOMINAL"
	if len(gaps) > 0 || len(missingPolicies) > 0 {
		classification = "POLICY_DRIFT"
	}

	integrationOK := false
	if cfg.APIKey != "" {
		integrationOK, _ = client.IntegrationSitesHaveID(ctx)
	}

	return map[string]any{
		"classification":         classification,
		"auth_mode":              "SESSION_PATH",
		"controller_version":     version,
		"integration_key_usable": integrationOK,
		"zone_binding_gaps":      gaps,
		"missing_policies":       missingPolicies,
		"bifrost_policy_count":   countBifrostPolicies(policies),
		"expected_policy_count":  len(expectedPolicyNames),
		"zone_specs":             len(zoneSpecs),
	}, nil
}

func sysinfoVersion(raw json.RawMessage) (string, error) {
	items, err := parseJSONArray(raw)
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", nil
	}
	version, _ := items[0]["version"].(string)
	return version, nil
}

func countBifrostPolicies(policies []map[string]any) int {
	n := 0
	for _, p := range policies {
		if strings.HasPrefix(fmt.Sprint(p["name"]), "Bifrost |") {
			n++
		}
	}
	return n
}
