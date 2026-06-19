package stack

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/cluster"
	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func stackInstallEnabled() bool {
	v := os.Getenv("PLATFORM_STACK_INSTALL_ENABLED")
	if v == "0" || v == "false" || v == "no" {
		return false
	}
	return v == "1" || v == "true" || v == "yes" || v == ""
}

func (s *Service) InstallAddon(name string) (cluster.ActuationResponse, error) {
	return s.runAddonScript(name, "stack.install", true)
}

func (s *Service) UpgradeAddon(name string) (cluster.ActuationResponse, error) {
	return s.runAddonScript(name, "stack.upgrade", false)
}

func (s *Service) runAddonScript(name, action string, install bool) (cluster.ActuationResponse, error) {
	now := time.Now().UTC()
	id := strings.TrimSpace(strings.ToLower(name))
	ns := "cicd"
	if s.entry != nil {
		ns = s.entry.ResolvedStackNamespace()
	}
	target := fmt.Sprintf("%s/%s", ns, id)
	resp := cluster.ActuationResponse{
		OK:          false,
		Action:      action,
		Target:      target,
		GeneratedAt: now,
	}

	if id == "" {
		resp.Message = "addon name required"
		return resp, fmt.Errorf("%s", resp.Message)
	}

	if !stackInstallEnabled() {
		resp.Message = "stack install disabled (set PLATFORM_STACK_INSTALL_ENABLED=1)"
		return resp, nil
	}

	spec, ok := s.resolveAddonSpec(id)
	if !ok {
		resp.Message = fmt.Sprintf("unknown stack addon %q", id)
		return resp, fmt.Errorf("%s", resp.Message)
	}

	script := spec.UpgradeScript
	if install || script == "" {
		script = spec.InstallScript
	}
	if script == "" {
		resp.Message = fmt.Sprintf("no install script configured for addon %q", id)
		return resp, fmt.Errorf("%s", resp.Message)
	}

	if err := s.cluster.RunInfraK3sScript(script, nil); err != nil {
		resp.Message = fmt.Sprintf("%s failed: %s", action, err.Error())
		return resp, err
	}

	resp.OK = true
	resp.Changed = true
	resp.Message = fmt.Sprintf("%s completed for %s via %s", action, spec.Label, script)
	return resp, nil
}

func (s *Service) resolveAddonSpec(id string) (config.StackAddonSpec, bool) {
	specs := []config.StackAddonSpec{
		{ID: "registry", Label: "Registry", Match: "registry", InstallScript: "install-stack-registry.sh", UpgradeScript: "install-stack-registry.sh"},
		{ID: "gitea", Label: "Gitea", Match: "gitea", InstallScript: "install-gitea-persistent.sh", UpgradeScript: "install-gitea-persistent.sh"},
		{ID: "tekton", Label: "Tekton", Match: "tekton", InstallScript: "install-stack-tekton.sh", UpgradeScript: "install-stack-tekton.sh"},
	}
	if s.entry != nil && len(s.entry.Stack.Addons) > 0 {
		specs = s.entry.Stack.Addons
	}
	for _, spec := range specs {
		if strings.EqualFold(strings.TrimSpace(spec.ID), id) {
			out := spec
			if out.InstallScript == "" {
				out.InstallScript = defaultInstallScript(out.ID)
			}
			if out.UpgradeScript == "" {
				out.UpgradeScript = out.InstallScript
			}
			return out, true
		}
	}
	return config.StackAddonSpec{}, false
}

func defaultInstallScript(id string) string {
	switch strings.ToLower(strings.TrimSpace(id)) {
	case "registry":
		return "install-stack-registry.sh"
	case "gitea":
		return "install-gitea-persistent.sh"
	case "tekton":
		return "install-stack-tekton.sh"
	default:
		return ""
	}
}
