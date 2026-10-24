package main

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
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
	named := func(key string) bool {
		return exec.Command("plutil", "-extract", key, "raw", "-o", "-", plist).Run() == nil
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
	if value("EnvironmentVariables.AGENTD_CURSOR_KEY_FILE") != "" {
		t.Error("no cursor key file, yet one is named")
	}
	if value("EnvironmentVariables.AGENTD_CLAUDE_TOKEN_FILE") != "" {
		t.Error("no claude token file, yet one is named")
	}
	// Claude's token, when there, is named; one others can read is refused.
	claudeToken := filepath.Join(config, "claude-oauth.token")
	if err = os.WriteFile(claudeToken, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr != nil {
		t.Fatalf("render with a claude token: %v\n%s", rerr, out)
	}
	if got := value("EnvironmentVariables.AGENTD_CLAUDE_TOKEN_FILE"); got != claudeToken {
		t.Errorf("AGENTD_CLAUDE_TOKEN_FILE = %q, want %q", got, claudeToken)
	}
	if err = os.Chmod(claudeToken, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "must be mode 0600") {
		t.Fatalf("a claude token readable by others: %v %s", rerr, out)
	}
	if err = os.Remove(claudeToken); err != nil {
		t.Fatal(err)
	}
	// Cursor's key, when there, is named; one others can read is refused.
	cursorKey := filepath.Join(config, "cursor-api.key")
	if err = os.WriteFile(cursorKey, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr != nil {
		t.Fatalf("render with a cursor key: %v\n%s", rerr, out)
	}
	if got := value("EnvironmentVariables.AGENTD_CURSOR_KEY_FILE"); got != cursorKey {
		t.Errorf("AGENTD_CURSOR_KEY_FILE = %q, want %q", got, cursorKey)
	}
	if err = os.Chmod(cursorKey, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "must be mode 0600") {
		t.Fatalf("a cursor key readable by others: %v %s", rerr, out)
	}
	if err = os.WriteFile(cursorKey, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(cursorKey, 0o600); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "is empty") {
		t.Fatalf("an empty cursor key (review 196 B2): %v %s", rerr, out)
	}
	if err = os.WriteFile(cursorKey, []byte(" \n\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr == nil || !strings.Contains(out, "is empty") {
		t.Fatalf("a cursor key of blanks only (review 197 B2): %v %s", rerr, out)
	}
	if err = os.Remove(cursorKey); err != nil {
		t.Fatal(err)
	}
	if out, rerr := render(bin); rerr != nil {
		t.Fatalf("render again: %v\n%s", rerr, out)
	}
	// The push base is gone (cut §9.8): branches go back to the git base, so
	// one left in the installer's environment does not reach the plist.
	push := exec.Command("bash", script, "--render", plist, me.Username, bin, platform, "h1", "exec:codex=openai", "https://git.example/x&y")
	push.Env = append(os.Environ(), "AGENTD_CONFIG_DIR="+config, "AGENTD_ROOT_DIR="+root, "AGENTD_GIT_PUSH_BASE=https://git.example/agents")
	if out, perr := push.CombinedOutput(); perr != nil {
		t.Fatalf("render with a leftover push base: %v\n%s", perr, out)
	}
	if named("EnvironmentVariables.AGENTD_GIT_PUSH_BASE") {
		t.Errorf("AGENTD_GIT_PUSH_BASE = %q: a leftover push base reached the plist", value("EnvironmentVariables.AGENTD_GIT_PUSH_BASE"))
	}
	if got := value("EnvironmentVariables.AGENTD_GIT_BASE"); got != "https://git.example/x&y" {
		t.Errorf("AGENTD_GIT_BASE = %q", got)
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

// 10-15: a reinstall booted out a busy agentd and bootstrapped before launchd
// had let go of it; the bootstrap failed and the host had no agentd. The
// installer's own lines from bootout to bootstrap, run against a stub
// launchctl: the plist is replaced and bootstrapped only once print says the
// service is not there (exit 113); still there for a minute, or a print that
// fails otherwise, stops with the old plist in place (review 203 A1).
func TestInstallWaitsForLaunchdToLetGo(t *testing.T) {
	script, err := os.ReadFile("../../../agent/deploy/install-agentd.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	// The commands at the start of a line, not their names in a message.
	from := strings.Index(text, "\nlaunchctl bootout ") + 1
	to := strings.Index(text, "\nlaunchctl bootstrap ") + 1
	if from == 0 || to <= from {
		t.Fatal("the installer has no bootout followed by a bootstrap")
	}
	end := strings.IndexByte(text[to:], '\n')
	snippet := text[from : to+end+1]
	stub := `#!/bin/sh
echo "$*" >> "$DIR/calls"
[ "$1" = print ] || exit 0
n=$(cat "$DIR/n" 2>/dev/null || echo 0); n=$((n + 1)); echo "$n" > "$DIR/n"
[ "$n" -le "$LOADED_FOR" ] && exit 0
echo "Could not find service" >&2
exit "$GONE_RC"
`
	for _, c := range []struct {
		name               string
		loadedFor, goneRC  int
		ok                 bool
		prints, bootstraps int
	}{
		{"gone at once", 0, 113, true, 1, 1},
		{"gone after three seconds", 3, 113, true, 4, 1},
		{"never gone", 1000, 113, false, 61, 0},
		{"print fails", 0, 5, false, 1, 0},
		{"print fails after waiting", 2, 64, false, 3, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "launchctl"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			plist, tmp := filepath.Join(dir, "agentd.plist"), filepath.Join(dir, "new.plist")
			if err := os.WriteFile(plist, []byte("old"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(tmp, []byte("new"), 0o600); err != nil {
				t.Fatal(err)
			}
			harness := "set -euo pipefail\ndie() { echo \"$*\" >&2; exit 1; }\nsleep() { :; }\n" +
				"label=com.bifrost.agentd plist=\"$PLIST\" tmp=\"$TMP\"\n" + snippet
			cmd := exec.Command("bash", "-c", harness)
			cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "DIR="+dir, "PLIST="+plist, "TMP="+tmp,
				"LOADED_FOR="+strconv.Itoa(c.loadedFor), "GONE_RC="+strconv.Itoa(c.goneRC))
			out, err := cmd.CombinedOutput()
			if (err == nil) != c.ok {
				t.Fatalf("err = %v, want ok %v\n%s", err, c.ok, out)
			}
			calls, _ := os.ReadFile(filepath.Join(dir, "calls"))
			if got := strings.Count(string(calls), "print "); got != c.prints {
				t.Errorf("prints = %d, want %d", got, c.prints)
			}
			if got := strings.Count(string(calls), "bootstrap "); got != c.bootstraps {
				t.Errorf("bootstraps = %d, want %d", got, c.bootstraps)
			}
			want := "old"
			if c.ok {
				want = "new"
			}
			if got, _ := os.ReadFile(plist); string(got) != want {
				t.Errorf("plist = %q, want %q", got, want)
			}
		})
	}
}
