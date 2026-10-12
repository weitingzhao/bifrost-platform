package releasefreeze

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"k8s.io/client-go/rest"
)

const randomFixedSeed int64 = 31020261011

const (
	randomSequences = 5000
	randomSteps     = 40
	pythonSequences = 500
)

const pyGateSrc = `
import json, sys
sys.path.insert(0, sys.argv[1])
import freeze_gate
for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    req = json.loads(line)
    d = freeze_gate.read_freeze(req["url"], req["token"], req["ca"])
    sys.stdout.write(json.dumps({"verdict": d.verdict, "first": d.lines[0]}) + "\n")
    sys.stdout.flush()
`

type cluster struct {
	mu     sync.Mutex
	w      world
	rules  rules
	fault  int
	left   int
	badGet int
}

func (c *cluster) reset() {
	c.mu.Lock()
	c.w = world{}
	c.left = 0
	c.mu.Unlock()
}

func (c *cluster) snapshot() world {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.w
}

func (c *cluster) arm(fault, n int) {
	c.mu.Lock()
	c.fault = fault
	c.left = n
	c.mu.Unlock()
}

func (c *cluster) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v1/release-policy/unfreeze" {
		UnfreezeHandler(w, r)
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(r.Body, 1<<20))
	c.mu.Lock()
	defer c.mu.Unlock()
	if r.Method == http.MethodGet && r.URL.Path == ListPath {
		if r.URL.RawQuery != "" {
			c.badGet++
		}
		if c.left > 0 {
			writeFault(w, uint8(c.fault))
			c.left--
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(listBody(c.w))
		return
	}
	ev, ok := parseEvent(r.Header.Get("X-Model-Event"))
	if !ok {
		ev = event{kind: evCreate, who: idP, class: classBadSpec}
	}
	next, refused := step(c.rules, c.w, ev)
	if !refused {
		c.w = next
	}
	if refused {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func writeFault(w http.ResponseWriter, kind uint8) {
	switch kind {
	case faultNon200:
		http.Error(w, string(listBody(world{})), http.StatusInternalServerError)
	case fault403:
		http.Error(w, "forbidden", http.StatusForbidden)
	case fault404:
		http.Error(w, "missing", http.StatusNotFound)
	case faultContinue:
		writeJSON(w, `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"continue":"tok"},"items":[]}`)
	case faultKind:
		writeJSON(w, `{"apiVersion":"release.bifrost.io/v1","kind":"Freeze","metadata":{},"items":[]}`)
	case faultItems:
		writeJSON(w, `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{},"items":{}}`)
	default:
		writeJSON(w, `{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{},"items":[{}]}`)
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, body)
}

func eventHeader(e event) string {
	return fmt.Sprintf("%d %d %d %d %d %d %d %d %d %d",
		e.kind, e.who, e.name, e.class, e.field, e.uidMode, e.prop, e.gate, e.lin, e.fault)
}

func parseEvent(s string) (event, bool) {
	var e event
	n, err := fmt.Sscanf(s, "%d %d %d %d %d %d %d %d %d %d",
		&e.kind, &e.who, &e.name, &e.class, &e.field, &e.uidMode, &e.prop, &e.gate, &e.lin, &e.fault)
	return e, err == nil && n == 10
}

type recorded struct {
	method string
	url    string
	body   string
	header string
	gone   bool
}

type pyChild struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	sc     *bufio.Scanner
	stderr bytes.Buffer
}

func startPy(t *testing.T, dir string) *pyChild {
	t.Helper()
	cmd := exec.Command("python3", "-u", "-c", pyGateSrc, dir)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	ch := &pyChild{cmd: cmd, in: stdin}
	cmd.Stderr = &ch.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ch.sc = bufio.NewScanner(stdout)
	return ch
}

func (p *pyChild) read(url, token, ca string) (string, string, error) {
	if _, err := io.WriteString(p.in, fmt.Sprintf("{\"url\":%q,\"token\":%q,\"ca\":%q}\n", url, token, ca)); err != nil {
		return "", "", err
	}
	if !p.sc.Scan() {
		return "", "", fmt.Errorf("python closed: %v %s", p.sc.Err(), p.stderr.String())
	}
	var resp struct {
		Verdict string `json:"verdict"`
		First   string `json:"first"`
	}
	if err := json.Unmarshal(p.sc.Bytes(), &resp); err != nil {
		return "", "", fmt.Errorf("%w (%s)", err, p.stderr.String())
	}
	return resp.Verdict, resp.First, nil
}

func (p *pyChild) stop() {
	_ = p.in.Close()
	_ = p.cmd.Process.Kill()
	_, _ = p.cmd.Process.Wait()
}

func siblingReleaseDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	dir := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "bifrost-trade-infra", "scripts", "release"))
	if _, err := os.Stat(filepath.Join(dir, "freeze_gate.py")); err != nil {
		if os.Getenv("BIFROST_CI") != "" {
			t.Fatalf("sibling freeze_gate.py missing: %v", err)
		}
		t.Skipf("sibling freeze_gate.py absent: %v", err)
	}
	if _, err := exec.LookPath("python3"); err != nil {
		if os.Getenv("BIFROST_CI") != "" {
			t.Fatal("python3 absent")
		}
		t.Skip("python3 absent")
	}
	return dir
}

func TestRandomSequences(t *testing.T) {
	dir := siblingReleaseDir(t)
	cl := &cluster{rules: correctRules(), fault: -1}
	srv := httptest.NewUnstartedServer(cl)
	srv.EnableHTTP2 = false
	srv.StartTLS()
	defer srv.Close()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	tmp := t.TempDir()
	caFile := filepath.Join(tmp, "ca.pem")
	tokenFile := filepath.Join(tmp, "token")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenFile, []byte("model-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader := testRESTFull(t, srv.URL, caPEM, tokenFile)
	writer := srv.Client()
	py := startPy(t, dir)
	defer py.stop()

	var seedBuf [8]byte
	if _, err := rand.Read(seedBuf[:]); err != nil {
		t.Fatal(err)
	}
	runtimeSeed := int64(binary.LittleEndian.Uint64(seedBuf[:]) & 0x7fffffffffffffff)
	t.Logf("seed fixed %d", randomFixedSeed)
	t.Logf("seed runtime %d", runtimeSeed)

	start := time.Now()
	replays := 0
	pyReads, pySeqs := runSequences(t, cl, writer, reader, py, srv.URL, tokenFile, caFile, randomFixedSeed, randomSequences, &replays)
	extraReads, extraSeqs := runSequences(t, cl, writer, reader, py, srv.URL, tokenFile, caFile, runtimeSeed, 1, &replays)
	pyReads += extraReads
	pySeqs += extraSeqs
	if cl.badGet != 0 {
		t.Fatalf("%d GET requests carried a query", cl.badGet)
	}
	t.Logf("python sequences %d", pySeqs)
	t.Logf("python reads %d", pyReads)
	t.Logf("replays %d", replays)
	t.Logf("random seconds %.3f", time.Since(start).Seconds())
}

func runSequences(t *testing.T, cl *cluster, writer *http.Client, reader rest.Interface, py *pyChild, base, token, ca string, seed int64, n int, replays *int) (int, int) {
	t.Helper()
	rng := mrand.New(mrand.NewSource(seed))
	all := alphabet()
	reused := Gate{}
	listURL := base + ListPath
	pyReads := 0
	pySeqs := 0
	oldBody := `{"text":"unfreeze frozen_at=2026-10-10T09:00:00Z","sig":"abc"}`
	for seq := 0; seq < n; seq++ {
		cl.reset()
		usePy := seq < pythonSequences
		if usePy {
			pySeqs++
		}
		var pending []recorded
		for stepN := 0; stepN < randomSteps; stepN++ {
			snap := cl.snapshot()
			var armed = -1
			switch {
			case len(pending) > 0 && rng.Intn(3) == 0:
				rec := pending[0]
				pending = pending[1:]
				doRecorded(t, writer, rec)
				*replays++
			case rng.Intn(12) == 0:
				rec := recorded{method: http.MethodPost, url: base + "/api/v1/release-policy/unfreeze", body: oldBody, gone: true}
				doRecorded(t, writer, rec)
				pending = append(pending, rec)
				bad := recorded{method: http.MethodPost, url: base + ListPath, body: oldBody}
				doRecorded(t, writer, bad)
				pending = append(pending, bad)
			default:
				evs := eventsFor(all, snap)
				e := evs[rng.Intn(len(evs))]
				switch e.kind {
				case evRead:
				case evFault:
					if e.fault != faultTimeout {
						armed = int(e.fault)
					}
				default:
					rec := recordFor(base, snap, e)
					doRecorded(t, writer, rec)
					pending = append(pending, rec)
				}
			}
			if rng.Intn(2) == 0 {
				continue
			}
			reads := 2
			if usePy {
				reads = 3
			}
			if armed >= 0 {
				cl.arm(armed, reads)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			first := reused.Read(ctx, reader)
			second := Gate{}.Read(ctx, reader)
			cancel()
			if first.Verdict != second.Verdict || first.Lines[0] != second.Lines[0] {
				t.Fatalf("reused and fresh gates differ: %+v vs %+v", first, second)
			}
			if usePy {
				pv, pf, err := py.read(listURL, token, ca)
				if err != nil {
					t.Fatal(err)
				}
				pyReads++
				if pv != first.Verdict || pf != first.Lines[0] {
					t.Fatalf("seq %d step %d armed %d python %s %q, go %s %q", seq, stepN, armed, pv, pf, first.Verdict, first.Lines[0])
				}
			}
			after := cl.snapshot()
			if armed >= 0 {
				if first.Verdict == VerdictClear {
					t.Fatal("U: fault read reported clear")
				}
			} else if msg := liveCheck(after, first); msg != "" {
				t.Fatalf("%s after %s", msg, formatPath(nil))
			}
			if rng.Intn(2) == 0 && after.create && oldAllows(after) {
				t.Fatal("S: old program would allow after create is enabled")
			}
		}
		for _, rec := range pending {
			doRecorded(t, writer, rec)
			*replays++
		}
	}
	return pyReads, pySeqs
}

func liveCheck(w world, d Decision) string {
	if !anyPresent(w) && d.Verdict != VerdictClear {
		return "L"
	}
	if anyPresent(w) && d.Verdict != VerdictFrozen {
		return "present record was not frozen"
	}
	if d.Verdict == VerdictClear && !allDeletedByO(w) {
		return "S"
	}
	return ""
}

func recordFor(base string, w world, e event) recorded {
	name := modelNames[e.name]
	rec := recorded{header: eventHeader(e), body: "{}"}
	switch e.kind {
	case evCreate:
		rec.method = http.MethodPost
		rec.url = base + ListPath
		rec.body = fmt.Sprintf(`{"apiVersion":"release.bifrost.io/v1","kind":"Freeze","metadata":{"name":"%s"},"spec":{"who":"x","reason":"y","at":"2026-10-11T00:00:00Z"}}`, name)
	case evUpdate:
		rec.url = base + ListPath + "/" + name
		rec.method = http.MethodPut
		if e.field == updMeta {
			rec.method = http.MethodPatch
		}
		if e.field == updSub {
			rec.url += "/status"
		}
		rec.body = `{"spec":{"reason":"changed"}}`
	case evDelete:
		rec.method = http.MethodDelete
		rec.url = base + ListPath + "/" + name
		pol := "Background"
		if e.prop == propForeground {
			pol = "Foreground"
		}
		if e.uidMode == uidAbsent {
			rec.body = fmt.Sprintf(`{"kind":"DeleteOptions","apiVersion":"v1","propagationPolicy":"%s"}`, pol)
		} else {
			uid := "uid-stale"
			if e.uidMode == uidMatch {
				uid = fmt.Sprintf("uid-%d-%d", e.name, w.names[e.name].n)
			}
			rec.body = fmt.Sprintf(`{"kind":"DeleteOptions","apiVersion":"v1","preconditions":{"uid":"%s"},"propagationPolicy":"%s"}`, uid, pol)
		}
	case evDeleteCollection:
		rec.method = http.MethodDelete
		rec.url = base + ListPath
	default:
		rec.method = http.MethodPost
		rec.url = base + "/model/control"
	}
	return rec
}

func doRecorded(t *testing.T, client *http.Client, rec recorded) {
	t.Helper()
	req, err := http.NewRequest(rec.method, rec.url, bytes.NewReader([]byte(rec.body)))
	if err != nil {
		t.Fatal(err)
	}
	if rec.header != "" {
		req.Header.Set("X-Model-Event", rec.header)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if rec.gone {
		if resp.StatusCode != http.StatusGone || string(got) != UnfreezeGoneBody {
			t.Fatalf("unfreeze route %d %s", resp.StatusCode, got)
		}
	}
}
