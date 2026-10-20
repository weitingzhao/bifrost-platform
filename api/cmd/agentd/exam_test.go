package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// agentd exam says what it cannot sit before it takes the host or asks for
// a paper: no job or executor, an executor that sits no exam, one this host
// does not run.
func TestExamRefusesWhatItCannotSit(t *testing.T) {
	var out bytes.Buffer
	log.SetOutput(&out)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	token := filepath.Join(t.TempDir(), "agentd.token")
	if err := os.WriteFile(token, []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for k, v := range map[string]string{"AGENTD_PLATFORM": "https://p.example/api/v1/agentrt", "AGENTD_HOST": "h1",
		"AGENTD_TOKEN_FILE": token, "AGENTD_TOOLS": "exec:codex=openai", "AGENTD_GIT_BASE": "https://g.example/bifrost",
		"AGENTD_ROOT": t.TempDir(), "HOME": t.TempDir()} {
		t.Setenv(k, v)
	}
	for _, c := range []struct {
		args []string
		code int
		says string
	}{
		{nil, 2, "usage"},
		{[]string{"--job", "chief_of_staff"}, 2, "usage"},
		{[]string{"--job", "chief_of_staff", "--executor", "exec:codex"}, 1, "only exec:claude sits an exam"},
		{[]string{"--job", "chief_of_staff", "--executor", "exec:claude"}, 1, "exec:claude is not in AGENTD_TOOLS"},
	} {
		out.Reset()
		if code := runExam(c.args); code != c.code || !strings.Contains(out.String(), c.says) {
			t.Errorf("agentd exam %v: %d, %q", c.args, code, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config", "bifrost", "agentd.lock")); err == nil {
		t.Error("a refused exam took the host")
	}
}
