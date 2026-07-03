package network

import (
	"os"
	"path/filepath"
)

// resolvePlatformScript locates a bifrost-platform script from cwd or PLATFORM_PROJECT_ROOT.
func resolvePlatformScript(rel string) string {
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

	if root := os.Getenv("PLATFORM_PROJECT_ROOT"); root != "" {
		if p, ok := try(filepath.Join(root, rel)); ok {
			return p
		}
	}

	if wd, err := os.Getwd(); err == nil {
		for _, base := range []string{wd, filepath.Join(wd, ".."), filepath.Join(wd, "../..")} {
			if p, ok := try(filepath.Join(base, rel)); ok {
				return p
			}
			if p, ok := try(filepath.Join(base, "bifrost-platform", rel)); ok {
				return p
			}
		}
	}

	return filepath.Join("..", rel)
}
