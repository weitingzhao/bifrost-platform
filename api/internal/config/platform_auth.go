package config

import (
	"os"
	"path/filepath"
)

func ResolvePlatformAuthPath(configDir string) string {
	if path := os.Getenv("PLATFORM_AUTH_CONFIG"); path != "" {
		return path
	}
	if configDir != "" {
		return filepath.Join(configDir, "platform-auth.yaml")
	}
	if wd, err := os.Getwd(); err == nil {
		for _, p := range []string{
			filepath.Join(wd, "config", "platform-auth.yaml"),
			filepath.Join(wd, "..", "config", "platform-auth.yaml"),
		} {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return "config/platform-auth.yaml"
}
