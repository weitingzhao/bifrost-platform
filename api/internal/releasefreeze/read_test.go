package releasefreeze

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func mintCert(dnsName string) (caPEM []byte, server tls.Certificate, err error) {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	caPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	leaf := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: dnsName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{dnsName},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		return nil, tls.Certificate{}, err
	}
	server, err = tls.X509KeyPair(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}),
	)
	return caPEM, server, err
}

type rawServer struct {
	addr string
	hits []string
	mu   sync.Mutex
	ln   net.Listener
}

func (s *rawServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.hits...)
	return out
}

func startRaw(t *testing.T, serverCert tls.Certificate, protos []string, respond func(n int, req string) []byte) *rawServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{serverCert}, NextProtos: protos, MinVersion: tls.VersionTLS12}
	tlsLn := tls.NewListener(ln, cfg)
	s := &rawServer{addr: ln.Addr().String(), ln: tlsLn}
	go func() {
		n := 0
		for {
			conn, err := tlsLn.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn, n int) {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, 4096)
				k, _ := conn.Read(buf)
				req := string(buf[:k])
				s.mu.Lock()
				s.hits = append(s.hits, req)
				s.mu.Unlock()
				_, _ = conn.Write(respond(n, req))
			}(conn, n)
			n++
		}
	}()
	t.Cleanup(func() { _ = tlsLn.Close() })
	return s
}

func resp(status, extra string, body []byte) []byte {
	head := "HTTP/1.1 " + status + "\r\n" + extra
	if body != nil && !strings.Contains(extra, "Transfer-Encoding") && !strings.Contains(extra, "Content-Length") {
		head += "Content-Length: " + strconv.Itoa(len(body)) + "\r\n"
	}
	if !strings.HasSuffix(head, "\r\n") {
		head += "\r\n"
	}
	head += "\r\n"
	return append([]byte(head), body...)
}

func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func writeCA(t *testing.T, pemBytes []byte) string {
	t.Helper()
	path := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func gateFor(t *testing.T, addr, ca string) Gate {
	t.Helper()
	return Gate{Addr: addr, CAFile: ca, Token: "gate-token"}
}

func mustClear(t *testing.T, d Decision) {
	t.Helper()
	if d.Verdict != VerdictClear {
		t.Fatalf("verdict %s reason %s line %q", d.Verdict, d.Reason, firstLine(d))
	}
}

func mustRefuse(t *testing.T, d Decision) {
	t.Helper()
	if d.Verdict == VerdictClear {
		t.Fatalf("was clear: %+v", d)
	}
}

func firstLine(d Decision) string {
	if len(d.Lines) == 0 {
		return ""
	}
	return d.Lines[0]
}

func TestTransportHTTP11Settings(t *testing.T) {
	tr := http1Transport(nil)
	if tr.ForceAttemptHTTP2 {
		t.Fatal("ForceAttemptHTTP2")
	}
	if tr.TLSNextProto == nil || len(tr.TLSNextProto) != 0 {
		t.Fatal("TLSNextProto")
	}
	if tr.TLSClientConfig == nil || !reflect.DeepEqual(tr.TLSClientConfig.NextProtos, []string{"http/1.1"}) {
		t.Fatal(tr.TLSClientConfig.NextProtos)
	}
	if tr.TLSClientConfig.ServerName != OriginHost || tr.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("tls name")
	}
	if _, err := tr.Proxy(nil); err != nil {
		t.Fatal(err)
	}
}

func TestReadClearOverHTTP11(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	body := EmptyListBody()
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return resp("200 OK", "Content-Type: application/json\r\n", body)
	})
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("https_proxy", "http://127.0.0.1:1")
	got := gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context())
	mustClear(t, got)
	reqs := s.requests()
	if len(reqs) != 1 {
		t.Fatalf("requests %d", len(reqs))
	}
	if !strings.HasPrefix(reqs[0], "GET "+ListPath+" HTTP/1.1\r\n") {
		t.Fatalf("request line %q", reqs[0])
	}
	if strings.Contains(reqs[0], "?") {
		t.Fatalf("query in %q", reqs[0])
	}
	if !strings.Contains(reqs[0], "Accept: application/json\r\n") || !strings.Contains(reqs[0], "Accept-Encoding: identity\r\n") {
		t.Fatalf("headers %q", reqs[0])
	}
	if !strings.Contains(reqs[0], "Authorization: Bearer gate-token\r\n") {
		t.Fatal("token")
	}
}

func TestReadDoesNotFollowARedirect(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	s := startRaw(t, cert, []string{"http/1.1"}, func(n int, _ string) []byte {
		if n == 0 {
			return resp("302 Found", "Location: /elsewhere\r\nContent-Length: 0\r\n", nil)
		}
		return resp("200 OK", "", EmptyListBody())
	})
	got := gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context())
	mustRefuse(t, got)
	if len(s.requests()) != 1 {
		t.Fatalf("followed redirect, requests %d", len(s.requests()))
	}
}

func TestReadRejectsContentEncoding(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	headers := []string{
		"Content-Encoding: gzip\r\n",
		"content-encoding: identity\r\n",
		"Content-Encoding:\r\nContent-Encoding: gzip\r\n",
	}
	for _, h := range headers {
		s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
			return resp("200 OK", h, EmptyListBody())
		})
		got := gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context())
		mustRefuse(t, got)
	}
}

func TestReadBodyLimit(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	pad := func(n int) []byte {
		b := append([]byte(nil), EmptyListBody()...)
		for len(b) < n {
			b = append(b, ' ')
		}
		return b[:n]
	}
	okBody := pad(4096)
	if !MemberOfL(okBody) {
		t.Fatal("4096 padding left L")
	}
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return resp("200 OK", "", okBody)
	})
	mustClear(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))
	big := pad(4097)
	s2 := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return resp("200 OK", "", big)
	})
	mustRefuse(t, gateFor(t, s2.addr, writeCA(t, ca)).Read(t.Context()))
}

func TestReadTruncatedContentLength(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	body := EmptyListBody()
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		head := "HTTP/1.1 200 OK\r\nContent-Length: " + strconvItoa(len(body)+8) + "\r\n\r\n"
		return append([]byte(head), body...)
	})
	mustRefuse(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))
}

func TestReadChunked(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	body := EmptyListBody()
	chunk := func(extra string) []byte {
		return []byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n" +
			strconvHex(len(body)) + "\r\n" + string(body) + "\r\n" + extra)
	}
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return chunk("0\r\n\r\n")
	})
	mustClear(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))

	s2 := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return chunk("0\r\n")
	})
	mustRefuse(t, gateFor(t, s2.addr, writeCA(t, ca)).Read(t.Context()))

	s3 := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return []byte("HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n" +
			strconvHex(len(body)) + "\r\n" + string(body) + "\r\n")
	})
	mustRefuse(t, gateFor(t, s3.addr, writeCA(t, ca)).Read(t.Context()))
}

func strconvHex(n int) string {
	const digits = "0123456789abcdef"
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = digits[n%16]
		n /= 16
	}
	return string(b[i:])
}

func TestReadInterimResponses(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	body := EmptyListBody()
	for _, code := range []string{"100 Continue", "102 Processing", "103 Early Hints"} {
		s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
			pre := "HTTP/1.1 " + code + "\r\n\r\n"
			return append([]byte(pre), resp("200 OK", "", body)...)
		})
		mustClear(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))
	}
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return append([]byte("HTTP/1.1 101 Switching Protocols\r\n\r\n"), resp("200 OK", "", body)...)
	})
	mustRefuse(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))
}

func TestReadWrongHostname(t *testing.T) {
	ca, cert, err := mintCert("wrong.example")
	if err != nil {
		t.Fatal(err)
	}
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return resp("200 OK", "", EmptyListBody())
	})
	mustRefuse(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))
}

func TestReadH2OnlyIsNotClear(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	s := startRaw(t, cert, []string{"h2"}, func(int, string) []byte {
		return resp("200 OK", "", EmptyListBody())
	})
	mustRefuse(t, gateFor(t, s.addr, writeCA(t, ca)).Read(t.Context()))
}

func TestReadSlowDripUsesOneDeadline(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	body := EmptyListBody()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &tls.Config{Certificates: []tls.Certificate{cert}, NextProtos: []string{"http/1.1"}, MinVersion: tls.VersionTLS12}
	tlsLn := tls.NewListener(ln, cfg)
	t.Cleanup(func() { _ = tlsLn.Close() })
	go func() {
		conn, err := tlsLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4096)
		_, _ = conn.Read(buf)
		payload := resp("200 OK", "", body)
		for _, b := range payload {
			time.Sleep(15 * time.Millisecond)
			if _, err := conn.Write([]byte{b}); err != nil {
				return
			}
		}
	}()
	g := gateFor(t, ln.Addr().String(), writeCA(t, ca))
	g.Budget = 200 * time.Millisecond
	start := time.Now()
	got := g.Read(t.Context())
	mustRefuse(t, got)
	if time.Since(start) > 2*time.Second {
		t.Fatalf("drip waited %s", time.Since(start))
	}
}

func TestReadFullBudgetConstant(t *testing.T) {
	if readBudget != 10*time.Second {
		t.Fatalf("budget %s", readBudget)
	}
}

func TestFaultInjectionSeams(t *testing.T) {
	ca, cert, err := mintCert(OriginHost)
	if err != nil {
		t.Fatal(err)
	}
	s := startRaw(t, cert, []string{"http/1.1"}, func(int, string) []byte {
		return resp("200 OK", "", EmptyListBody())
	})
	caPath := writeCA(t, ca)
	dialFail := Gate{Addr: s.addr, CAFile: caPath, Token: "t", Dial: func(context.Context, string, string, *tls.Config, time.Time) (net.Conn, error) {
		return nil, errors.New("dial failed")
	}}
	mustRefuse(t, dialFail.Read(t.Context()))

	readerFail := gateFor(t, s.addr, caPath)
	readerFail.Wrap = func(io.Reader) io.Reader { return failReader{} }
	mustRefuse(t, readerFail.Read(t.Context()))

	panicGate := gateFor(t, s.addr, caPath)
	panicGate.Hook = func() { panic("hook") }
	mustRefuse(t, panicGate.Read(t.Context()))
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestUnfreezeHandlerIs410(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/release-policy/unfreeze", strings.NewReader(`{"text":"unfreeze","sig":"abc"}`))
	rr := httptest.NewRecorder()
	UnfreezeHandler(rr, req)
	if rr.Code != http.StatusGone || rr.Body.String() != UnfreezeGoneBody {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
}

func TestImportGraph(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	cmd := exec.Command("go", "list", "-deps", ".")
	cmd.Dir = strings.TrimSuffix(file, "/read_test.go")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		for _, bad := range []string{
			"k8s.io/client-go/tools/cache",
			"k8s.io/client-go/tools/pager",
			"k8s.io/client-go/informers",
		} {
			if line == bad || strings.HasPrefix(line, bad+"/") {
				t.Fatalf("import graph contains %s", line)
			}
		}
	}
}

func TestMissingTokenAndCA(t *testing.T) {
	g := Gate{TokenFile: t.TempDir() + "/nope", CAFile: t.TempDir() + "/nope"}
	if g.Read(t.Context()).Verdict == VerdictClear {
		t.Fatal("missing token was clear")
	}
	dir := t.TempDir()
	tok := dir + "/token"
	if err := os.WriteFile(tok, []byte("t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g = Gate{TokenFile: tok, CAFile: dir + "/missing"}
	d := g.Read(t.Context())
	if d.Verdict == VerdictClear || d.Reason != "missing CA" {
		t.Fatalf("%+v", d)
	}
}
