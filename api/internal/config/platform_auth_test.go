package config

import (
	"path/filepath"
	"testing"
)

func TestResolvePlatformAuthPath(t *testing.T) {
	if got := ResolvePlatformAuthPath("/a/b"); got != filepath.Join("/a/b", "platform-auth.yaml") {
		t.Fatalf("ResolvePlatformAuthPath(configDir) = %q", got)
	}

	t.Setenv("PLATFORM_AUTH_CONFIG", "/custom/auth.yaml")
	if got := ResolvePlatformAuthPath("/a/b"); got != "/custom/auth.yaml" {
		t.Fatalf("ResolvePlatformAuthPath() env override = %q, want /custom/auth.yaml", got)
	}
}
