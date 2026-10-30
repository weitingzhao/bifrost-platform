package infracheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSetupScriptPreservesArguments(t *testing.T) {
	for _, wheels := range []string{"", "/opt/wheels", "/tmp/it's a wheelhouse; $(false)"} {
		t.Run(wheels, func(t *testing.T) {
			dir := t.TempDir()
			venv := filepath.Join(dir, infraVenv, "bin")
			if err := os.MkdirAll(venv, 0700); err != nil {
				t.Fatal(err)
			}
			// Capture literal argv instead of creating a venv or installing packages.
			for path, script := range map[string]string{
				filepath.Join(dir, "python3"):  "#!/bin/sh\nprintf '%s\\0' \"$@\" > venv-args\nexit \"${VENV_EXIT:-0}\"\n",
				filepath.Join(venv, "python3"): "#!/bin/sh\nprintf '%s\\0' \"$PIP_CONFIG_FILE\" \"$@\" > pip-args\n",
			} {
				if err := os.WriteFile(path, []byte(script), 0700); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("sh", "-c", SetupScript(wheels))
			cmd.Dir, cmd.Env = dir, append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			pip := []string{"/dev/null", "-I", "-m", "pip", "--isolated", "install", "--quiet", "--no-cache-dir", "--no-input", "--disable-pip-version-check", "--only-binary=:all:", "--no-deps"}
			if wheels != "" {
				pip = append(pip, "--no-index", "--find-links", wheels)
			}
			pip = append(pip, "pytest==9.0.2", "pluggy==1.6.0", "iniconfig==2.1.0", "packaging==25.0", "pygments==2.19.2", "pyyaml==6.0.3")
			for name, want := range map[string][]string{"venv-args": {"-I", "-m", "venv", "--clear", ".git/venv"}, "pip-args": pip} {
				raw, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || !reflect.DeepEqual(strings.Split(strings.TrimSuffix(string(raw), "\x00"), "\x00"), want) {
					t.Fatalf("%s: %q (%v), want %q", name, raw, err, want)
				}
			}
			if err := os.Remove(filepath.Join(dir, "pip-args")); err != nil {
				t.Fatal(err)
			}
			cmd = exec.Command("sh", "-c", SetupScript(wheels))
			cmd.Dir, cmd.Env = dir, append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "VENV_EXIT=7")
			if err := cmd.Run(); err == nil {
				t.Fatal("setup ignored failed venv creation")
			}
			if _, err := os.Stat(filepath.Join(dir, "pip-args")); !os.IsNotExist(err) {
				t.Fatal("pip ran after failed venv creation", err)
			}
		})
	}
}
