package releasefreeze

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const randomFixedSeed int64 = 31020261011

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
    clear = d.verdict == "clear"
    sys.stdout.write(json.dumps({"clear": clear}) + "\n")
    sys.stdout.flush()
`

func randomPlan() (mem, live, py int) {
	mem, live, py = 2000, 20, 3
	if os.Getenv("BIFROST_FREEZE_FULL") == "1" {
		return 5000, 40, 8
	}
	if v := os.Getenv("FREEZE_MODEL_SEQUENCES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			mem = n
		}
	}
	return mem, live, py
}

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
	c.fault = -1
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

func (c *cluster) serve(method, path, query, header string) (int, []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if method == http.MethodGet && path == ListPath {
		if query != "" {
			c.badGet++
		}
		if c.left > 0 {
			c.left--
			return faultResponse(uint8(c.fault))
		}
		return http.StatusOK, listBody(c.w)
	}
	if path == "/api/v1/release-policy/unfreeze" {
		return http.StatusGone, []byte(UnfreezeGoneBody)
	}
	ev, ok := parseEvent(header)
	if !ok {
		return http.StatusForbidden, []byte("no event")
	}
	next, refused := step(c.rules, c.w, ev)
	if !refused {
		c.w = next
	}
	if refused {
		return http.StatusForbidden, []byte("refused")
	}
	return http.StatusOK, []byte("ok")
}

func faultResponse(kind uint8) (int, []byte) {
	if kind == faultBody {
		return http.StatusOK, []byte(`{"apiVersion":"release.bifrost.io/v1","kind":"FreezeList","metadata":{"resourceVersion":"1","resourceVersion":"2"},"items":[]}`)
	}
	return http.StatusInternalServerError, EmptyListBody()
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

func liveCheck(w world, d Decision) string {
	clear := d.Verdict == VerdictClear
	if !anyPresent(w) && !clear {
		return "L"
	}
	if anyPresent(w) && clear {
		return "present record was clear"
	}
	if clear && !allDeletedByO(w) {
		return "S"
	}
	return ""
}

func runMemory(t *testing.T, seed int64, n int) {
	t.Helper()
	rng := rand.New(rand.NewSource(seed))
	all := alphabet()
	rules := correctRules()
	for seq := 0; seq < n; seq++ {
		w := world{}
		for stepN := 0; stepN < 40; stepN++ {
			evs := eventsFor(all, w)
			e := evs[rng.Intn(len(evs))]
			switch e.kind {
			case evRead:
				d := DecideHTTP(200, listBody(w))
				if msg := liveCheck(w, d); msg != "" {
					t.Fatalf("memory %s seq %d step %d", msg, seq, stepN)
				}
			case evFault:
				d := faultDecision(e.fault)
				if d.Verdict == VerdictClear {
					t.Fatalf("U: fault read reported clear seq %d", seq)
				}
			default:
				next, refused := step(rules, w, e)
				if !refused {
					w = next
				}
			}
		}
	}
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

func (p *pyChild) clear(url, token, ca string) (bool, error) {
	if _, err := io.WriteString(p.in, fmt.Sprintf("{\"url\":%q,\"token\":%q,\"ca\":%q}\n", url, token, ca)); err != nil {
		return false, err
	}
	if !p.sc.Scan() {
		return false, fmt.Errorf("python closed: %v %s", p.sc.Err(), p.stderr.String())
	}
	var resp struct {
		Clear bool `json:"clear"`
	}
	if err := json.Unmarshal(p.sc.Bytes(), &resp); err != nil {
		return false, fmt.Errorf("%w (%s)", err, p.stderr.String())
	}
	return resp.Clear, nil
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

func startModelServer(t *testing.T, cl *cluster) (addr, caFile string) {
	t.Helper()
	caPEM, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"http/1.1"},
		MinVersion:   tls.VersionTLS12,
	}
	tlsLn := tls.NewListener(ln, cfg)
	t.Cleanup(func() { _ = tlsLn.Close() })
	go func() {
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				method, path, query, header, err := readHTTPRequest(conn)
				if err != nil {
					return
				}
				status, body := cl.serve(method, path, query, header)
				_, _ = conn.Write(resp(strconv.Itoa(status)+" "+http.StatusText(status), "", body))
			}(conn)
		}
	}()
	dir := t.TempDir()
	caFile = filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caFile, caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	return ln.Addr().String(), caFile
}

func readHTTPRequest(conn net.Conn) (method, path, query, header string, err error) {
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return "", "", "", "", err
	}
	parts := strings.Fields(strings.TrimRight(line, "\r\n"))
	if len(parts) < 2 {
		return "", "", "", "", fmt.Errorf("request line")
	}
	method = parts[0]
	target := parts[1]
	path, query, _ = strings.Cut(target, "?")
	var clen int
	for {
		h, err := br.ReadString('\n')
		if err != nil {
			return "", "", "", "", err
		}
		h = strings.TrimRight(h, "\r\n")
		if h == "" {
			break
		}
		name, value, ok := strings.Cut(h, ":")
		if !ok {
			continue
		}
		name = strings.ToLower(strings.TrimSpace(name))
		value = strings.TrimSpace(value)
		if name == "content-length" {
			clen, _ = strconv.Atoi(value)
		}
		if name == "x-model-event" {
			header = value
		}
	}
	if clen > 0 {
		buf := make([]byte, clen)
		if _, err := io.ReadFull(br, buf); err != nil {
			return "", "", "", "", err
		}
	}
	return method, path, query, header, nil
}

func TestRandomSequences(t *testing.T) {
	mem, live, pyN := randomPlan()
	start := time.Now()
	runMemory(t, randomFixedSeed, mem)
	t.Logf("seed %d", randomFixedSeed)
	t.Logf("sequences %d", mem)
	t.Logf("memory seconds %.3f", time.Since(start).Seconds())

	dir := siblingReleaseDir(t)
	cl := &cluster{rules: correctRules(), fault: -1}
	addr, caFile := startModelServer(t, cl)
	tokenFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokenFile, []byte("model-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	pool := loadPool(t, caFile)
	writer := &http.Client{Transport: &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
		TLSClientConfig: &tls.Config{
			RootCAs:    pool,
			ServerName: OriginHost,
			NextProtos: []string{"http/1.1"},
			MinVersion: tls.VersionTLS12,
		},
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		DisableKeepAlives: true,
	}}
	base := "https://" + addr
	py := startPy(t, dir)
	defer py.stop()
	session := tls.NewLRUClientSessionCache(128)
	reused := Gate{Addr: addr, CAFile: caFile, Token: "model-token", Session: session}
	listURL := base + ListPath
	pyReads := 0
	liveStart := time.Now()
	rng := rand.New(rand.NewSource(randomFixedSeed))
	all := alphabet()
	for seq := 0; seq < live; seq++ {
		cl.reset()
		usePy := seq < pyN
		for stepN := 0; stepN < 40; stepN++ {
			evs := eventsFor(all, cl.snapshot())
			e := evs[rng.Intn(len(evs))]
			reads := 1
			if usePy {
				reads = 2
			}
			switch e.kind {
			case evRead, evFault:
			default:
				doModelWrite(t, writer, base, e)
			}
			if rng.Intn(2) == 0 {
				continue
			}
			armed := -1
			if e.kind == evFault {
				armed = int(e.fault)
				cl.arm(armed, reads)
			}
			ctx := t.Context()
			first := reused.Read(ctx)
			if armed < 0 && seq == 0 && stepN == 0 {
				fresh := Gate{Addr: addr, CAFile: caFile, Token: "model-token"}.Read(ctx)
				if (first.Verdict == VerdictClear) != (fresh.Verdict == VerdictClear) {
					t.Fatalf("reused and fresh differ: %+v vs %+v", first, fresh)
				}
			}
			if usePy {
				got, err := py.clear(listURL, tokenFile, caFile)
				if err != nil {
					t.Fatal(err)
				}
				pyReads++
				if got != (first.Verdict == VerdictClear) {
					t.Fatalf("seq %d step %d armed %d python clear %v go %s", seq, stepN, armed, got, first.Verdict)
				}
			}
			after := cl.snapshot()
			if armed >= 0 {
				if first.Verdict == VerdictClear {
					t.Fatal("U: fault read reported clear")
				}
			} else if msg := liveCheck(after, first); msg != "" {
				t.Fatalf("%s seq %d step %d verdict %s", msg, seq, stepN, first.Verdict)
			}
		}
	}
	if cl.badGet != 0 {
		t.Fatalf("%d GET requests carried a query", cl.badGet)
	}
	t.Logf("live sequences %d", live)
	t.Logf("python sequences %d", pyN)
	t.Logf("python reads %d", pyReads)
	t.Logf("random seconds %.3f", time.Since(liveStart).Seconds())
}

func loadPool(t *testing.T, caFile string) *x509.CertPool {
	t.Helper()
	return mustPool(t, caFile)
}

func mustPool(t *testing.T, caFile string) *x509.CertPool {
	t.Helper()
	pool, err := loadCA(caFile)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func doModelWrite(t *testing.T, client *http.Client, base string, e event) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+"/model/control", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Model-Event", eventHeader(e))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
