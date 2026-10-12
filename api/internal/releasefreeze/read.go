package releasefreeze

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// OriginHost is the only host the gate dials unless a test supplies Addr.
	OriginHost = "kubernetes.default.svc"
	// TokenPath is the pod service-account token. Tests pass Token or TokenFile.
	TokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	// CAPath is the pod CA. Tests pass CAFile.
	CAPath = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

	readBudget = 10 * time.Second
)

// Gate is a stateless HTTP/1.1 reader. A value kept across calls stores nothing it has observed.
type Gate struct {
	// Addr is host:port. Empty dials kubernetes.default.svc:443.
	Addr string
	// CAFile is the PEM CA. Empty uses CAPath.
	CAFile string
	// Token is the bearer token. When empty, TokenFile is read.
	Token string
	// TokenFile defaults to TokenPath.
	TokenFile string
	// Budget is the one deadline from dial to the last body byte. Zero means 10s.
	Budget time.Duration
	// Dial replaces the TLS dial. Slices 2 and 3 inject a failing dialer here.
	Dial func(ctx context.Context, network, addr string, cfg *tls.Config, deadline time.Time) (net.Conn, error)
	// Wrap replaces the response reader. A failing reader returns errors from Read.
	Wrap func(io.Reader) io.Reader
	// Hook runs in the calling goroutine before the dial. A panic is recovered here.
	Hook func()
	// Session resumes TLS handshakes when a test dials the same server many times.
	Session tls.ClientSessionCache
}

// Read performs one GET. Every recoverable failure is not clear.
func (g Gate) Read(ctx context.Context) (d Decision) {
	defer func() {
		if recover() != nil {
			d = failClosed("panic")
		}
	}()
	if g.Hook != nil {
		g.Hook()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	got, err := g.read(ctx)
	if err != nil {
		return failClosed(transportToken(err))
	}
	return got
}

func (g Gate) budget() time.Duration {
	if g.Budget > 0 {
		return g.Budget
	}
	return readBudget
}

func transportToken(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timeout"
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "timeout"
	}
	msg := err.Error()
	switch msg {
	case "missing token", "missing CA", "corrupt CA", "timeout":
		return msg
	}
	if strings.Contains(strings.ToLower(msg), "timeout") || strings.Contains(msg, "deadline") {
		return "timeout"
	}
	return "connection failed"
}

// http1Transport is the HTTP/1.1 client configuration. The dial uses its TLSClientConfig.
func http1Transport(pool *x509.CertPool) *http.Transport {
	return &http.Transport{
		Proxy:              func(*http.Request) (*url.URL, error) { return nil, nil },
		ForceAttemptHTTP2:  false,
		TLSNextProto:       make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
		DisableCompression: true,
		TLSClientConfig: &tls.Config{
			RootCAs:            pool,
			ServerName:         OriginHost,
			NextProtos:         []string{"http/1.1"},
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false, // hostname and chain are verified
		},
	}
}

func (g Gate) read(ctx context.Context) (Decision, error) {
	token, err := g.token()
	if err != nil {
		return Decision{}, err
	}
	pool, err := loadCA(g.caFile())
	if err != nil {
		return Decision{}, err
	}
	deadline := time.Now().Add(g.budget())
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	tr := http1Transport(pool)
	if g.Session != nil {
		tr.TLSClientConfig.ClientSessionCache = g.Session
	}
	addr := g.Addr
	if addr == "" {
		addr = OriginHost + ":443"
	}
	var conn net.Conn
	if g.Dial != nil {
		conn, err = g.Dial(ctx, "tcp", addr, tr.TLSClientConfig, deadline)
	} else {
		conn, err = defaultDial(ctx, addr, tr.TLSClientConfig, deadline)
	}
	if err != nil {
		return Decision{}, err
	}
	defer conn.Close()

	armDeadline(conn, deadline)
	req := "GET " + ListPath + " HTTP/1.1\r\n" +
		"Host: " + OriginHost + "\r\n" +
		"Accept: application/json\r\n" +
		"Accept-Encoding: identity\r\n" +
		"Authorization: Bearer " + token + "\r\n" +
		"Connection: close\r\n" +
		"\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		return Decision{}, err
	}
	var src io.Reader = conn
	if g.Wrap != nil {
		src = g.Wrap(conn)
	}
	br := bufio.NewReader(src)
	return readHTTP(br, conn, deadline, token, g, ctx, pool)
}

func armDeadline(conn net.Conn, deadline time.Time) {
	_ = conn.SetDeadline(deadline) // one total deadline
}

func defaultDial(ctx context.Context, addr string, cfg *tls.Config, deadline time.Time) (net.Conn, error) {
	raw, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	armDeadline(raw, deadline)
	tlsConn := tls.Client(raw, cfg)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if p := tlsConn.ConnectionState().NegotiatedProtocol; p != "http/1.1" {
		_ = tlsConn.Close()
		return nil, errors.New("connection failed")
	}
	return tlsConn, nil
}

func (g Gate) token() (string, error) {
	if g.Token != "" {
		return strings.TrimSpace(g.Token), nil
	}
	path := g.TokenFile
	if path == "" {
		path = TokenPath
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("missing token")
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" {
		return "", errors.New("missing token")
	}
	return tok, nil
}

func (g Gate) caFile() string {
	if g.CAFile != "" {
		return g.CAFile
	}
	return CAPath
}

func loadCA(path string) (*x509.CertPool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("missing CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("corrupt CA")
	}
	return pool, nil
}

func followRedirect() bool { return false }

func encodingForbidden(present bool) bool { return present }

func chunkFrameComplete(finalCRLF bool) bool { return finalCRLF }

func lengthComplete(got, want int) bool { return got == want }

func readHTTP(br *bufio.Reader, conn net.Conn, deadline time.Time, token string, g Gate, ctx context.Context, pool *x509.CertPool) (Decision, error) {
	interim := 0
	for {
		armDeadline(conn, deadline)
		head, err := readHead(br, conn, deadline)
		if err != nil {
			return Decision{}, err
		}
		if encodingForbidden(len(head.values("content-encoding")) > 0) {
			return notClear("content-encoding"), nil
		}
		if head.status == 100 || head.status == 102 || head.status == 103 {
			interim++
			if interim > maxInterim {
				return notClear("too many interim responses"), nil
			}
			continue
		}
		if head.status == 101 || (head.status >= 100 && head.status < 200) {
			return notClear("HTTP " + strconv.Itoa(head.status)), nil
		}
		if head.status >= 300 && head.status < 400 {
			if !followRedirect() {
				return notClear("HTTP " + strconv.Itoa(head.status)), nil
			}
			return followOnce(head, token, g, ctx, pool, deadline)
		}
		body, err := readBody(br, conn, deadline, head)
		if err != nil {
			return Decision{}, err
		}
		return DecideHTTP(head.status, body), nil
	}
}

func followOnce(head respHead, token string, g Gate, ctx context.Context, pool *x509.CertPool, deadline time.Time) (Decision, error) {
	loc := ""
	if vs := head.values("location"); len(vs) > 0 {
		loc = strings.TrimSpace(vs[0])
	}
	path := loc
	if u, err := url.Parse(loc); err == nil && u.Path != "" {
		path = u.Path
	}
	if path == "" || !strings.HasPrefix(path, "/") {
		return notClear("HTTP 302"), nil
	}
	tr := http1Transport(pool)
	addr := g.Addr
	if addr == "" {
		addr = OriginHost + ":443"
	}
	var conn net.Conn
	var err error
	if g.Dial != nil {
		conn, err = g.Dial(ctx, "tcp", addr, tr.TLSClientConfig, deadline)
	} else {
		conn, err = defaultDial(ctx, addr, tr.TLSClientConfig, deadline)
	}
	if err != nil {
		return Decision{}, err
	}
	defer conn.Close()
	armDeadline(conn, deadline)
	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + OriginHost + "\r\n" +
		"Accept: application/json\r\n" +
		"Accept-Encoding: identity\r\n" +
		"Authorization: Bearer " + token + "\r\n" +
		"Connection: close\r\n\r\n"
	if _, err := io.WriteString(conn, req); err != nil {
		return Decision{}, err
	}
	br := bufio.NewReader(conn)
	h2, err := readHead(br, conn, deadline)
	if err != nil {
		return Decision{}, err
	}
	body, err := readBody(br, conn, deadline, h2)
	if err != nil {
		return Decision{}, err
	}
	return DecideHTTP(h2.status, body), nil
}
