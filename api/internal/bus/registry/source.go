package registry

import (
	"io/fs"
	"os"
	"path/filepath"
)

// Where the register is read from (2026-11-20, the Owner: a module that joins
// or leaves must not mean a code change, nor wait for a release). The files
// compiled in are the default; BUS_REGISTRY_DIR, when set, names a directory
// laid out the same way (<tenant>.yaml, subscriptions/, watches/), typically a
// ConfigMap mounted from the same Git files, and each part found there wins
// over the compiled one. A part missing there is read from the compiled files.

// RegistryDirEnv names the directory that overrides the compiled register.
const RegistryDirEnv = "BUS_REGISTRY_DIR"

// source is the file system a part of the register is read from: the
// override directory when it holds that part, else the compiled files.
func source(compiled fs.FS, part string) fs.FS {
	dir := os.Getenv(RegistryDirEnv)
	if dir == "" {
		return compiled
	}
	probe := dir
	if part != "." {
		probe = filepath.Join(dir, part)
	}
	if st, err := os.Stat(probe); err != nil || !st.IsDir() {
		return compiled
	}
	if part == "." {
		if m, _ := filepath.Glob(filepath.Join(dir, "*.yaml")); len(m) == 0 {
			return compiled
		}
	}
	return os.DirFS(dir)
}

// Source says where the register is read from, for the Bus page and logs.
func Source() string {
	if dir := os.Getenv(RegistryDirEnv); dir != "" {
		return dir
	}
	return "compiled"
}
