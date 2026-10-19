package agentd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A codex login as codex keeps it (keys read 10-19, codex 0.162); fixture
// values only.
func loginJSON(t *testing.T, account, refresh, when string, extra map[string]any) []byte {
	t.Helper()
	doc := map[string]any{"auth_mode": "chatgpt", "OPENAI_API_KEY": nil, "last_refresh": when,
		"tokens": map[string]any{"id_token": "fixture.id." + refresh, "access_token": "fixture.access." + refresh,
			"refresh_token": "fixture-refresh-" + refresh, "account_id": account}}
	for k, v := range extra {
		doc[k] = v
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCodexHome(t *testing.T) {
	for env, want := range map[string]string{
		"CODEX_HOME=/c HOME=/h": "/c",
		"HOME=/h":               "/h/.codex",
		"PATH=/bin":             "", // never the test's own user's home
	} {
		if got := CodexHome(strings.Fields(env)); got != want {
			t.Errorf("%s: %q, want %q", env, got, want)
		}
	}
}

func TestCopyCodexLogin(t *testing.T) {
	master, home := t.TempDir(), t.TempDir()
	if err := copyCodexLogin(master, home); err != nil {
		t.Fatalf("no login on the host: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "auth.json")); err == nil {
		t.Fatal("a login from nowhere")
	}
	body := loginJSON(t, "acct-1", "r1", "2026-10-19T01:00:00Z", nil)
	must(t, os.WriteFile(filepath.Join(master, "auth.json"), body, 0o600))
	must(t, copyCodexLogin(master, home))
	got, err := os.ReadFile(filepath.Join(home, "auth.json"))
	if err != nil || string(got) != string(body) {
		t.Fatalf("copy = %q %v", got, err)
	}
	if fi, _ := os.Stat(filepath.Join(home, "auth.json")); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	// The host's login is a link: not followed.
	linked := t.TempDir()
	must(t, os.Symlink(filepath.Join(master, "auth.json"), filepath.Join(linked, "auth.json")))
	if copyCodexLogin(linked, t.TempDir()) == nil {
		t.Fatal("a linked login was copied")
	}
}

// A refreshed login goes back to the host's, tokens and time only; anything
// else an attempt could put there does not.
func TestKeepCodexLogin(t *testing.T) {
	const host = "2026-10-19T01:00:00Z"
	later := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	for _, c := range []struct {
		name  string
		got   func(t *testing.T) []byte
		kept  bool
		error bool
	}{
		{"refreshed", func(t *testing.T) []byte { return loginJSON(t, "acct-1", "r2", later, nil) }, true, false},
		{"refreshed, with keys of its own", func(t *testing.T) []byte {
			return loginJSON(t, "acct-1", "r2", later, map[string]any{"auth_command": "sh -c x"})
		}, true, false},
		{"unchanged", func(t *testing.T) []byte { return loginJSON(t, "acct-1", "r1", host, nil) }, false, false},
		{"older", func(t *testing.T) []byte { return loginJSON(t, "acct-1", "r2", "2026-10-18T01:00:00Z", nil) }, false, false},
		{"another account", func(t *testing.T) []byte { return loginJSON(t, "acct-2", "r2", later, nil) }, false, true},
		{"an API key", func(t *testing.T) []byte {
			return loginJSON(t, "acct-1", "r2", later, map[string]any{"OPENAI_API_KEY": "fixture-key"})
		}, false, true},
		{"another mode", func(t *testing.T) []byte {
			return loginJSON(t, "acct-1", "r2", later, map[string]any{"auth_mode": "apikey"})
		}, false, true},
		{"in the future", func(t *testing.T) []byte {
			return loginJSON(t, "acct-1", "r2", time.Now().Add(time.Hour).UTC().Format(time.RFC3339), nil)
		}, false, true},
		{"not a token", func(t *testing.T) []byte { return loginJSON(t, "acct-1", "r2 x\n", later, nil) }, false, true},
		{"no tokens", func(t *testing.T) []byte {
			return []byte(`{"auth_mode":"chatgpt","OPENAI_API_KEY":null,"last_refresh":"` + later + `"}`)
		}, false, true},
		{"too big", func(t *testing.T) []byte {
			return append(loginJSON(t, "acct-1", "r2", later, nil), make([]byte, maxCodexLogin)...)
		}, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			master, home := t.TempDir(), t.TempDir()
			hostBody := loginJSON(t, "acct-1", "r1", host, map[string]any{"kept_by_codex": true})
			must(t, os.WriteFile(filepath.Join(master, "auth.json"), hostBody, 0o600))
			must(t, os.WriteFile(filepath.Join(home, "auth.json"), c.got(t), 0o600))
			err := keepCodexLogin(master, home)
			if (err != nil) != c.error {
				t.Fatalf("err = %v, want an error %v", err, c.error)
			}
			after, rerr := os.ReadFile(filepath.Join(master, "auth.json"))
			must(t, rerr)
			var doc map[string]any
			must(t, json.Unmarshal(after, &doc))
			toks, _ := doc["tokens"].(map[string]any)
			kept := toks["refresh_token"] == "fixture-refresh-r2"
			if kept != c.kept {
				t.Fatalf("kept = %v, want %v: %s", kept, c.kept, after)
			}
			if !c.kept && string(after) != string(hostBody) {
				t.Fatalf("the host's login changed: %s", after)
			}
			if c.kept {
				if doc["last_refresh"] != later || doc["kept_by_codex"] != true || doc["auth_command"] != nil || toks["account_id"] != "acct-1" {
					t.Fatalf("written back: %s", after)
				}
				if fi, _ := os.Stat(filepath.Join(master, "auth.json")); fi.Mode().Perm() != 0o600 {
					t.Fatalf("mode %v", fi.Mode())
				}
			}
		})
	}
	// The attempt's login is a link to another file: not read.
	master, home, other := t.TempDir(), t.TempDir(), t.TempDir()
	must(t, os.WriteFile(filepath.Join(master, "auth.json"), loginJSON(t, "acct-1", "r1", host, nil), 0o600))
	must(t, os.WriteFile(filepath.Join(other, "auth.json"), loginJSON(t, "acct-1", "r2", later, nil), 0o600))
	must(t, os.Symlink(filepath.Join(other, "auth.json"), filepath.Join(home, "auth.json")))
	if keepCodexLogin(master, home) == nil {
		t.Fatal("a linked login was taken")
	}
	// No login in the attempt: nothing to do.
	if err := keepCodexLogin(master, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

// codex run through its executor and after it: the login it was given, and
// the refreshed one taken back by afterExecutor; its usage read from its own
// home.
func TestCodexLoginRoundTrip(t *testing.T) {
	master := t.TempDir()
	must(t, os.WriteFile(filepath.Join(master, "auth.json"), loginJSON(t, "acct-1", "r1", "2026-10-19T01:00:00Z", nil), 0o600))
	out := t.TempDir()
	a := &Attempt{Claim: &Claimed{}, Spec: TaskSpec{Text: "# do it"}, Dir: t.TempDir(), Out: out}
	if _, err := Executors("/opt/agentd", []string{"PATH=/bin", "CODEX_HOME=" + master}, Secrets{})["exec:codex"](a); err != nil {
		t.Fatal(err)
	}
	later := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)
	must(t, os.WriteFile(filepath.Join(out, "home", ".codex", "auth.json"), loginJSON(t, "acct-1", "r2", later, nil), 0o600))
	afterExecutor("exec:codex", master, out, t.Logf)
	if b, _ := os.ReadFile(filepath.Join(master, "auth.json")); !strings.Contains(string(b), "fixture-refresh-r2") {
		t.Fatalf("the refreshed login is not the host's: %s", b)
	}

	const thread = "01a15031-53fc-7183-bce4-1bc9fbf576fb"
	day := filepath.Join(out, "home", ".codex", "sessions", "2026", "10", "19")
	must(t, os.MkdirAll(day, 0o700))
	must(t, os.WriteFile(filepath.Join(day, "rollout-2026-10-19T01-00-00-"+thread+".jsonl"),
		[]byte(`{"type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":4.0,"window_minutes":10080,"resets_at":1792874442},"secondary":null,"rate_limit_reached_type":null}}}`+"\n"), 0o600))
	must(t, os.WriteFile(filepath.Join(out, "log.txt"), []byte(`{"type":"thread.started","thread_id":"`+thread+`"}`+"\n"), 0o600))
	if m := measure("exec:codex", out); len(m.Pools) != 1 || *m.Pools[0].UsedFraction != 0.04 {
		t.Fatalf("pools from the attempt's codex home = %+v", m.Pools)
	}
}
