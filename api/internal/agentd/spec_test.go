package agentd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// Review 163 B1: a descendant that keeps the output open does not hold the
// command past its time (the descendant itself is ended with the attempt's
// group, TestVerifyStaysInTheAttemptGroup), and the output kept is bounded.
func TestVerifyReturnsOnTimeAndBoundsOutput(t *testing.T) {
	dir := t.TempDir()
	start := time.Now()
	v := Verify(context.Background(), dir, os.Environ(), [][]string{{"sh", "-c", "sleep 30 & wait"}}, 200*time.Millisecond)
	if v.Pass || !v.Commands[0].TimedOut || time.Since(start) > 5*time.Second {
		t.Fatalf("over the limit = %+v after %s", v, time.Since(start))
	}
	v = Verify(context.Background(), dir, os.Environ(), [][]string{{"sh", "-c", "head -c 10000000 /dev/zero | tr '\\\\0' x; echo end"}}, time.Minute)
	if !v.Pass || len(v.Commands[0].Tail) > tailBytes || !strings.HasSuffix(v.Commands[0].Tail, "end\n") {
		t.Fatalf("large output = %d bytes, pass %v", len(v.Commands[0].Tail), v.Pass)
	}
}

// Review 164 B1: the ring holds only its tail, whatever the writes.
func TestRingKeepsOnlyItsTail(t *testing.T) {
	r := &ring{max: 100}
	var all []byte
	for i := 0; i < 10000; i++ {
		chunk := []byte(fmt.Sprintf("%06d;", i))
		all = append(all, chunk...)
		if _, err := r.Write(chunk); err != nil {
			t.Fatal(err)
		}
		if len(r.buf) > r.max || cap(r.buf) > 2*r.max {
			t.Fatalf("after %d writes the ring holds %d bytes (cap %d)", i+1, len(r.buf), cap(r.buf))
		}
	}
	if string(r.bytes()) != string(all[len(all)-100:]) {
		t.Fatal("the ring does not hold the tail")
	}
	big := bytes.Repeat([]byte("x"), 1000)
	big[999] = 'y'
	if _, err := r.Write(big); err != nil || len(r.buf) != 100 || r.buf[99] != 'y' || cap(r.buf) > 2*r.max {
		t.Fatalf("a large write: %d bytes, cap %d", len(r.buf), cap(r.buf))
	}
}

// Review 164 B1: a report over its limit is not read, a log only by its tail.
func TestBoundedReads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big")
	body := bytes.Repeat([]byte("a"), 3<<20)
	copy(body[len(body)-5:], "tail!")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := readAtMost(path, maxBody); !errors.Is(err, errTooLarge) || b != nil {
		t.Fatalf("over the limit = %d bytes, %v", len(b), err)
	}
	if b, err := readAtMost(path, len(body)); err != nil || len(b) != len(body) {
		t.Fatalf("at the limit = %d bytes, %v", len(b), err)
	}
	if b, err := readTail(path, logTailBytes); err != nil || len(b) != logTailBytes || !bytes.HasSuffix(b, []byte("tail!")) {
		t.Fatalf("tail = %d bytes, %v", len(b), err)
	}
	if b, err := readTail(path, int64(len(body))*2); err != nil || len(b) != len(body) {
		t.Fatalf("a tail longer than the file = %d bytes, %v", len(b), err)
	}
	// What is read is what is kept: the bytes allocated, not only the result.
	if n := allocated(func() { _, _ = readTail(path, logTailBytes) }); n > 1<<20 {
		t.Fatalf("reading a 64 KiB tail allocated %d bytes", n)
	}
	if n := allocated(func() { _, _ = readAtMost(path, maxBody) }); n > 1<<20 {
		t.Fatalf("refusing a 3 MiB report allocated %d bytes", n)
	}
}

func allocated(fn func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	fn()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// A check of a salvaged result carries salvage (the platform's adopt.go);
// the spec reads it, and still refuses a field it does not know.
func TestParseSpecSalvage(t *testing.T) {
	s, err := ParseSpec([]byte(`{"text":"","check":"verify","subject_task":"t1","attempt":"a1","round":1,"task_spec":"sha256:x","salvage":true}`))
	if err != nil || !s.Salvage || s.Check != "verify" {
		t.Fatalf("spec = %+v %v", s, err)
	}
	if _, err = ParseSpec([]byte(`{"text":"","salvaged":true}`)); err == nil {
		t.Fatal("an unknown field read")
	}
}
