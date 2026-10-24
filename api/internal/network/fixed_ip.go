package network

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"

	"github.com/weitingzhao/bifrost-platform/api/internal/network/unifi"
)

var (
	ErrFixedIPRefused = errors.New("fixed IP refused")
	ErrClientNotFound = errors.New("UniFi client not found")
	hostNamePattern   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)
	clientIDPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
)

type FixedIPRequest struct {
	MAC  string `json:"mac"`
	IP   string `json:"ip"`
	Name string `json:"name"`
}

// ClientRecord deliberately exposes only address and naming information.
type ClientRecord struct {
	ID         string `json:"_id"`
	MAC        string `json:"mac"`
	IP         string `json:"ip"`
	UseFixedIP bool   `json:"use_fixedip"`
	FixedIP    string `json:"fixed_ip"`
	Name       string `json:"name"`
}

func normalizedMAC(value string) (string, error) {
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 {
		return "", fmt.Errorf("%w: mac must be a 48-bit MAC address", ErrFixedIPRefused)
	}
	return mac.String(), nil
}

func validateFixedIP(req FixedIPRequest, cfg unifi.Config) error {
	prefixText, gatewayText := cfg.ServerSubnet, cfg.ServerGateway
	if prefixText == "" {
		prefixText = unifi.DefaultServerSubnet
	}
	if gatewayText == "" {
		gatewayText = unifi.DefaultServerGateway
	}
	prefix, err := netip.ParsePrefix(prefixText)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() > 30 || prefix != prefix.Masked() {
		return fmt.Errorf("%w: UNIFI_SERVER_SUBNET must be a canonical IPv4 subnet with usable host addresses", ErrFixedIPRefused)
	}
	gateway, err := netip.ParseAddr(gatewayText)
	if err != nil || !gateway.Is4() || !prefix.Contains(gateway) || subnetBoundary(prefix, gateway) {
		return fmt.Errorf("%w: UNIFI_SERVER_GATEWAY must be a host address inside UNIFI_SERVER_SUBNET", ErrFixedIPRefused)
	}
	ip, err := netip.ParseAddr(req.IP)
	if err != nil || !ip.Is4() {
		return fmt.Errorf("%w: ip must be an IPv4 address", ErrFixedIPRefused)
	}
	if !prefix.Contains(ip) {
		return fmt.Errorf("%w: ip must be inside server subnet %s", ErrFixedIPRefused, prefix)
	}
	if subnetBoundary(prefix, ip) {
		return fmt.Errorf("%w: ip is the network or broadcast address", ErrFixedIPRefused)
	}
	if ip == gateway {
		return fmt.Errorf("%w: ip is the server gateway", ErrFixedIPRefused)
	}
	return nil
}

func subnetBoundary(prefix netip.Prefix, ip netip.Addr) bool {
	base, addr := prefix.Addr().As4(), ip.As4()
	broadcast := binary.BigEndian.Uint32(base[:]) | (^uint32(0) >> prefix.Bits())
	return ip == prefix.Addr() || binary.BigEndian.Uint32(addr[:]) == broadcast
}

// Legacy endpoints may return HTTP 200 with meta.rc=error. Never treat that as
// an empty client inventory or a successful write.
func checkLegacyResponse(raw json.RawMessage) error {
	var envelope struct {
		Meta struct {
			RC string `json:"rc"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("invalid UniFi legacy response: %w", err)
	}
	if envelope.Meta.RC != "ok" {
		return errors.New("UniFi legacy response did not report meta.rc=ok")
	}
	return nil
}

func readClientRecords(ctx context.Context, client *unifi.Client, path string) ([]ClientRecord, error) {
	raw, err := client.LegacyGet(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := checkLegacyResponse(raw); err != nil {
		return nil, err
	}
	var envelope struct {
		Data []ClientRecord `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("invalid UniFi client records: %w", err)
	}
	if envelope.Data == nil {
		return nil, errors.New("UniFi client response has no data array")
	}
	return envelope.Data, nil
}

func findClient(records []ClientRecord, mac string) (ClientRecord, error) {
	var found *ClientRecord
	for _, record := range records {
		candidate, err := normalizedMAC(record.MAC)
		if err != nil || candidate != mac {
			continue
		}
		if found != nil {
			return ClientRecord{}, fmt.Errorf("%w: multiple records for mac %s", ErrFixedIPRefused, mac)
		}
		record.MAC = candidate
		found = &record
	}
	if found == nil {
		return ClientRecord{}, fmt.Errorf("%w: %s", ErrClientNotFound, mac)
	}
	if !clientIDPattern.MatchString(found.ID) {
		return ClientRecord{}, fmt.Errorf("%w: client has no safe legacy record id", ErrFixedIPRefused)
	}
	return *found, nil
}

func (s *Service) ClientByMAC(ctx context.Context, mac string) (ClientRecord, error) {
	mac, err := normalizedMAC(mac)
	if err != nil {
		return ClientRecord{}, err
	}
	client, err := s.session(ctx)
	if err != nil {
		return ClientRecord{}, err
	}
	records, err := readClientRecords(ctx, client, "/rest/user")
	if err != nil {
		return ClientRecord{}, err
	}
	return findClient(records, mac)
}

func (s *Service) SetClientFixedIP(ctx context.Context, req FixedIPRequest) (map[string]any, error) {
	mac, err := normalizedMAC(req.MAC)
	if err != nil {
		return nil, err
	}
	if !hostNamePattern.MatchString(req.Name) {
		return nil, fmt.Errorf("%w: name must be a 1-63 character host name using letters, digits and internal hyphens", ErrFixedIPRefused)
	}
	req.MAC = mac
	s.fixedIPMu.Lock()
	defer s.fixedIPMu.Unlock()
	client, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateFixedIP(req, client.Config()); err != nil {
		return nil, err
	}
	known, err := readClientRecords(ctx, client, "/rest/user")
	if err != nil {
		return nil, err
	}
	before, err := findClient(known, mac)
	if err != nil {
		return nil, err
	}
	current, err := readClientRecords(ctx, client, "/stat/sta")
	if err != nil {
		return nil, err
	}
	for _, record := range append(known, current...) {
		other, _ := normalizedMAC(record.MAC)
		if other != mac && (record.IP == req.IP || record.FixedIP == req.IP) {
			return nil, fmt.Errorf("%w: ip %s is held by another known client (%s)", ErrFixedIPRefused, req.IP, record.MAC)
		}
	}
	result := map[string]any{"before": before, "no_op": false}
	if before.UseFixedIP && before.FixedIP == req.IP && before.Name == req.Name {
		result["after"], result["no_op"] = before, true
		return result, nil
	}
	path := "/rest/user/" + before.ID
	// Only these three fields may leave the platform in a write.
	raw, err := client.LegacyPut(ctx, path, map[string]any{"use_fixedip": true, "fixed_ip": req.IP, "name": req.Name})
	if err == nil {
		err = checkLegacyResponse(raw)
	}
	if err != nil {
		return result, fmt.Errorf("fixed IP write failed; controller state may have changed: %v", err)
	}
	records, err := readClientRecords(ctx, client, path)
	if err != nil {
		return result, fmt.Errorf("fixed IP readback failed after write: %v", err)
	}
	if len(records) != 1 {
		return result, errors.New("fixed IP readback mismatch: expected exactly one record")
	}
	after := records[0]
	result["after"] = after
	afterMAC, _ := normalizedMAC(after.MAC)
	if after.ID != before.ID || afterMAC != mac || !after.UseFixedIP || after.FixedIP != req.IP || after.Name != req.Name {
		return result, errors.New("fixed IP readback mismatch: saved record does not match the approved request")
	}
	return result, nil
}
