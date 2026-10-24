package unifi

import (
	"errors"
	"os"
)

// ErrNotConfigured proves no controller request can have started.
var ErrNotConfigured = errors.New("UNIFI_USER and UNIFI_PASS required (bifrost-agent local Super Admin)")

// Config matches scripts/unifi_firewall_setup.py env (Session v2 primary path).
type Config struct {
	Host          string
	User          string
	Pass          string
	APIKey        string
	Site          string
	ServerSubnet  string
	ServerGateway string
	ReservedIPs   string
}

const (
	defaultHost          = "192.168.1.1"
	defaultSite          = "default"
	DefaultServerSubnet  = "192.168.10.0/24"
	DefaultServerGateway = "192.168.10.1"
)

// ConfigFromEnv reads credentials, site and the server subnet/gateway and optional reserved IP list from UNIFI_* variables.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Host:          envOrDefault("UNIFI_HOST", defaultHost),
		User:          os.Getenv("UNIFI_USER"),
		Pass:          os.Getenv("UNIFI_PASS"),
		APIKey:        os.Getenv("UNIFI_API_KEY"),
		Site:          envOrDefault("UNIFI_SITE", defaultSite),
		ServerSubnet:  envOrDefault("UNIFI_SERVER_SUBNET", DefaultServerSubnet),
		ServerGateway: envOrDefault("UNIFI_SERVER_GATEWAY", DefaultServerGateway),
		ReservedIPs:   os.Getenv("UNIFI_RESERVED_IPS"),
	}
	if cfg.User == "" || cfg.Pass == "" {
		return Config{}, ErrNotConfigured
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
