package cluster

import (
	"os"
	"path/filepath"
)

// ResolveInfraScript locates a bifrost-trade-infra/scripts/k3s script from cwd or PLATFORM_PROJECT_ROOT.
func ResolveInfraScript(configured, scriptName string) string {
	infraRel := filepath.Join("bifrost-trade-infra", "scripts", "k3s", scriptName)

	try := func(p string) (string, bool) {
		p = filepath.Clean(p)
		if _, err := os.Stat(p); err == nil {
			if abs, err := filepath.Abs(p); err == nil {
				return abs, true
			}
			return p, true
		}
		return "", false
	}

	if configured != "" {
		if filepath.IsAbs(configured) {
			if p, ok := try(configured); ok {
				return p
			}
			return configured
		}
		if abs, err := filepath.Abs(configured); err == nil {
			if p, ok := try(abs); ok {
				return p
			}
		}
		if wd, err := os.Getwd(); err == nil {
			for _, base := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")} {
				if p, ok := try(filepath.Join(base, configured)); ok {
					return p
				}
			}
		}
	}

	if wd, err := os.Getwd(); err == nil {
		for _, base := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")} {
			if p, ok := try(filepath.Join(base, "..", infraRel)); ok {
				return p
			}
			if p, ok := try(filepath.Join(base, infraRel)); ok {
				return p
			}
		}
	}
	if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
		if p, ok := try(filepath.Join(root, "..", infraRel)); ok {
			return p
		}
	}
	return filepath.Join("..", "..", "bifrost-trade-infra", "scripts", "k3s", scriptName)
}
