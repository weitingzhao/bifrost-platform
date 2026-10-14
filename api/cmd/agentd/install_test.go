package main

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Review 164 B3: install-agentd.sh renders the plist one key at a time, so a
// path or URL with spaces, & or < comes out as it went in; the binary is
// named by its absolute path; credentials are checked before anything is
// written.
func TestInstallScriptRenders(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("plutil and dscl are macOS tools")
	}
	me, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs("../../../agent/deploy/install-agentd.sh")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(t.TempDir(), "a dir & <odd>")
	config, root, bindir := filepath.Join(base, "config"), filepath.Join(base, "root"), filepath.Join(base, "bin")
	for _, d := range []string{config, root, bindir} {
		if err = os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(bindir, "agentd")
	if err = os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	token := filepath.Join(config, "agentd.token")
	if err = os.WriteFile(token, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plist := filepath.Join(base, "out.plist")
	platform := "https://platform.example/api/v1/agentrt?a=1&b=<2>"
	render := func(binArg string) (string, error) {
		cmd := exec.Command("bash", script, "--render", plist, me.Username, binArg, platform, "h1", "exec:codex=openai:gpt-5", "https://git.example/x&y")
		cmd.Dir = bindir
		cmd.Env = append(os.Environ(), "AGENTD_CONFIG_DIR="+config, "AGENTD_ROOT_DIR="+root)
		out, rerr := cmd.CombinedOutput()
		return string(out), rerr
	}
	value := func(key string) string {
		out, xerr := exec.Command("plutil", "-extract", key, "raw", "-o", "-", plist).Output()
		if xerr != nil {
			t.Fatalf("%s: %v", key, xerr)
		}
		return strings.TrimSuffix(string(out), "\n")
	}

	if out, rerr := render("./agentd"); rerr != nil {
		t.Fatalf("render: %v\n%s", rerr, out)
	}
	realBin, _ := filepath.EvalSymlinks(bin)
	for key, want := range map[string]string{
		"UserName":                               me.Username,
		"ProgramArguments.0":                     realBin,
		"WorkingDirectory":                       root,
		"EnvironmentVariables.AGENTD_PLATFORM":   platform,
		"EnvironmentVariables.AGENTD_GIT_BASE":   "https://git.example/x&y",
		"EnvironmentVariables.AGENTD_TOKEN_FILE": token,
		"EnvironmentVariables.AGENTD_ROOT":       root,
		"StandardOutPath":                        filepath.Join(root, "agentd.log"),
	} {
		if got := value(key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if p := value("EnvironmentVariables.PATH"); !strings.HasPrefix(p, me.HomeDir+"/.local/bin:") {
		t.Errorf("PATH = %q: the user's ~/.local/bin first", p)
	}
	if value("EnvironmentVariables.AGENTD_GIT_TOKEN_FILE") != "" {
		t.Error("no git token file, yet one is named")
	}

	// A credential another can read, a link, or none: refused, the plist
	// left as it was.
	before, _ := os.ReadFile(plist)
	gitToken := filepath.Join(config, "agentd-git.token")
	if err = os.WriteFile(gitToken, []byte("fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "must be mode 0600") {
		t.Fatalf("a git token readable by others: %v %s", rerr, out)
	}
	if err = os.Remove(gitToken); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(token, gitToken); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "not a regular file") {
		t.Fatalf("a git token that is a link: %v %s", rerr, out)
	}
	if err = os.Remove(gitToken); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(token); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "missing:") {
		t.Fatalf("no agentd token: %v %s", rerr, out)
	}
	if after, _ := os.ReadFile(plist); string(after) != string(before) {
		t.Fatal("a refused render changed the plist")
	}
	if out, rerr := exec.Command("bash", script, "--render", plist, "no-such-user-agentd", bin, platform, "h1", "t", "g").CombinedOutput(); rerr == nil {
		t.Fatalf("an unknown user: %s", out)
	}
}
