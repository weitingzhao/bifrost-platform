package stack

import (
	"os"
	"testing"
)

func TestResolveAddonSpecDefaults(t *testing.T) {
	svc := NewService(nil)
	spec, ok := svc.resolveAddonSpec("registry")
	if !ok {
		t.Fatal("expected registry spec")
	}
	if spec.InstallScript != "install-stack-registry.sh" {
		t.Fatalf("install script: got %q", spec.InstallScript)
	}
}

func TestResolveAddonSpecUnknown(t *testing.T) {
	svc := NewService(nil)
	if _, ok := svc.resolveAddonSpec("unknown"); ok {
		t.Fatal("expected unknown addon")
	}
}

func TestInstallAddonDisabled(t *testing.T) {
	t.Setenv("PLATFORM_STACK_INSTALL_ENABLED", "0")
	svc := NewService(nil)
	resp, err := svc.InstallAddon("registry")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OK {
		t.Fatal("expected ok=false when disabled")
	}
	if resp.Action != "stack.install" {
		t.Fatalf("action: got %q", resp.Action)
	}
}

func TestInstallAddonUnknown(t *testing.T) {
	t.Setenv("PLATFORM_STACK_INSTALL_ENABLED", "1")
	svc := NewService(nil)
	resp, err := svc.InstallAddon("not-real")
	if err == nil {
		t.Fatal("expected error for unknown addon")
	}
	if resp.OK {
		t.Fatal("expected ok=false")
	}
}

func TestStackInstallEnabledDefault(t *testing.T) {
	_ = os.Unsetenv("PLATFORM_STACK_INSTALL_ENABLED")
	if !stackInstallEnabled() {
		t.Fatal("expected enabled by default")
	}
}
