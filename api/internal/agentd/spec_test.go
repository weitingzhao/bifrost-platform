package agentd

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseSpec(t *testing.T) {
	s, err := ParseSpec([]byte("# do the thing\n\nwith care\n"))
	if err != nil || s.Text != "# do the thing\n\nwith care\n" || len(s.Verify) != 0 {
		t.Fatalf("markdown = %+v %v", s, err)
	}
	s, err = ParseSpec([]byte(`{"text":"# x","verify":[["go","test","./..."],["make","lint"]],"timeout_s":60}`))
	if err != nil || len(s.Verify) != 2 || s.Verify[1][1] != "lint" || s.TimeoutS != 60 {
		t.Fatalf("json = %+v %v", s, err)
	}
	// the controller's check spec (store.checkSpec.put)
	s, err = ParseSpec([]byte(`{"attempt":"a_1","check":"verify","commit":"` + strings.Repeat("a", 40) + `","repo":"bifrost-platform","round":1,"subject_task":"t_1","task_spec":"sha256:` + strings.Repeat("b", 64) + `"}`))
	if err != nil || s.Check != "verify" || s.SubjectSpec == "" || s.Commit == "" || s.Round != 1 {
		t.Fatalf("check spec = %+v %v", s, err)
	}
	for _, bad := range []string{
		`{"text":"x","verify":[[]]}`,
		`{"text":"x","verify":[[""]]}`,
		`{"text":"x","shell":"rm -rf /"}`,
		`{"text":"x","timeout_s":-1}`,
		"\xff\xfe",
	} {
		if _, err := ParseSpec([]byte(bad)); err == nil {
			t.Errorf("took %q", bad)
		}
	}
}

func TestVerify(t *testing.T) {
	dir := t.TempDir()
	env := os.Environ()
	v := Verify(context.Background(), dir, env, [][]string{{"sh", "-c", "echo one"}, {"sh", "-c", "echo two; exit 3"}, {"touch", "never"}}, time.Minute)
	if v.Pass || len(v.Commands) != 2 || v.Commands[1].ExitCode != 3 || !strings.Contains(v.Commands[1].Tail, "two") {
		t.Fatalf("a failing command = %+v", v)
	}
	if _, err := os.Stat(dir + "/never"); err == nil {
		t.Fatal("a command after the failure ran")
	}
	if v = Verify(context.Background(), dir, env, [][]string{{"true"}, {"true"}}, time.Minute); !v.Pass || len(v.Commands) != 2 {
		t.Fatalf("passing = %+v", v)
	}
	if v = Verify(context.Background(), dir, env, nil, time.Minute); v.Pass {
		t.Fatal("no commands passed")
	}
	if v = Verify(context.Background(), dir, env, [][]string{{"no-such-command-agentd"}}, time.Minute); v.Pass {
		t.Fatal("a missing command passed")
	}
	start := time.Now()
	v = Verify(context.Background(), dir, env, [][]string{{"sleep", "30"}}, 200*time.Millisecond)
	if v.Pass || !v.Commands[0].TimedOut || time.Since(start) > 10*time.Second {
		t.Fatalf("over the limit = %+v after %s", v, time.Since(start))
	}
	long := strings.Repeat("é", 3000) // multibyte across the cut
	v = Verify(context.Background(), dir, env, [][]string{{"sh", "-c", "printf '%s' '" + long + "'"}}, time.Minute)
	if len(v.Commands[0].Tail) > tailBytes || !strings.HasSuffix(v.Commands[0].Tail, "é") || !utf8Valid(v.Commands[0].Tail) {
		t.Fatalf("tail = %d bytes", len(v.Commands[0].Tail))
	}
}

func utf8Valid(s string) bool { return strings.ToValidUTF8(s, "?") == s }
