package unifi

import (
	"fmt"
	"os"
)

// Config matches scripts/unifi_firewall_setup.py env (Session v2 primary path).
type Config struct {
	Host   string
	User   string
	Pass   string
	APIKey string
	Site   string
}

const (
	defaultHost = "192.168.1.1"
	defaultSite = "default"
)

// ConfigFromEnv reads UNIFI_HOST, UNIFI_USER, UNIFI_PASS, UNIFI_API_KEY.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		Host:   envOrDefault("UNIFI_HOST", defaultHost),
		User:   os.Getenv("UNIFI_USER"),
		Pass:   os.Getenv("UNIFI_PASS"),
		APIKey: os.Getenv("UNIFI_API_KEY"),
		Site:   envOrDefault("UNIFI_SITE", defaultSite),
	}
	if cfg.User == "" || cfg.Pass == "" {
		return Config{}, fmt.Errorf("UNIFI_USER and UNIFI_PASS required (bifrost-agent local Super Admin)")
	}
	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
