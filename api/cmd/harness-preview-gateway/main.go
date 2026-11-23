package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/harnesssession"
)

func main() {
	address := flag.String("listen", "", "private listener address")
	opsHost := flag.String("ops-host", "", "trusted Ops parent hostname")
	openCodeHost := flag.String("opencode-host", "", "OpenCode preview hostname")
	deepSeekHost := flag.String("deepseek-host", "", "DeepSeek preview hostname")
	openCodeSocket := flag.String("opencode-socket", "", "Unix socket of the isolated OpenCode backend")
	deepSeekSocket := flag.String("deepseek-socket", "", "Unix socket of the isolated DeepSeek backend")
	sessionKeyPath := flag.String("ops-session-public-key", "", "Ops Harness grant verifier")
	passwordHashPath := flag.String("password-sha256-file", "", "file containing the SHA-256 hash of the dedicated preview password")
	deepSeekLaunchPath := flag.String("deepseek-launch-url-file", "", "private file containing the native DeepSeek launch URL")
	flag.Parse()
	if *address == "" || *opsHost == "" || *openCodeHost == "" || *deepSeekHost == "" ||
		*opsHost == *openCodeHost || *opsHost == *deepSeekHost || *openCodeHost == *deepSeekHost {
		log.Fatal("configure a private listener and three distinct hostnames")
	}
	var passwordHash []byte
	var broker *sessions
	if *sessionKeyPath != "" {
		key, err := harnesssession.LoadVerifier(*sessionKeyPath)
		if err != nil {
			log.Fatal("invalid Ops Harness public key")
		}
		broker = newSessions(*opsHost, key, map[string]string{*openCodeHost: "opencode", *deepSeekHost: "deepseek"})
	} else {
		var err error
		passwordHash, err = hex.DecodeString(readValue(*passwordHashPath))
		if err != nil || len(passwordHash) != sha256.Size {
			log.Fatal("invalid preview password hash")
		}
	}
	launchURL, err := url.Parse(readValue(*deepSeekLaunchPath))
	if err != nil || launchURL.Query().Get("token") == "" {
		log.Fatal("invalid private DeepSeek launch URL")
	}
	apps := map[string]*httputil.ReverseProxy{
		*openCodeHost: newProxy("http://127.0.0.1:14143", *openCodeSocket, "", *opsHost),
		*deepSeekHost: newProxy("http://127.0.0.1:14144", *deepSeekSocket, launchURL.Query().Get("token"), *opsHost),
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app := apps[r.Host]
		if app == nil {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/.bifrost/") {
			if broker == nil {
				http.NotFound(w, r)
				return
			}
			broker.serve(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "https://"+r.Host {
			http.Error(w, "Origin not allowed", http.StatusForbidden)
			return
		}
		if _, supplied := r.URL.Query()["token"]; supplied {
			http.Error(w, "Tokenized URLs are unavailable on the preview", http.StatusBadRequest)
			return
		}
		if broker != nil {
			active, ok := broker.lookup(r)
			if !ok {
				w.Header().Set("Cache-Control", "no-store")
				http.Error(w, "Open this harness from Ops", http.StatusForbidden)
				return
			}
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			defer context.AfterFunc(active.context, cancel)()
			r = r.Clone(ctx)
			stripSessionCookie(r)
			app.ServeHTTP(w, r)
			return
		}
		user, password, ok := r.BasicAuth()
		hash := sha256.Sum256([]byte(password))
		if !ok || user != "preview" || subtle.ConstantTimeCompare(hash[:], passwordHash) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="4090 Harness Preview", charset="UTF-8"`)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "Preview sign-in required", http.StatusUnauthorized)
			return
		}
		app.ServeHTTP(w, r)
	})
	server := &http.Server{Addr: *address, Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func readValue(path string) string {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		log.Fatal("preview credential file must be a regular owner-only file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatal("could not read preview credential file")
	}
	return strings.TrimSpace(string(data))
}

func newProxy(target, socket, nativeToken, parent string) *httputil.ReverseProxy {
	upstream, _ := url.Parse(target)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	if socket != "" {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socket)
		}
	}
	return &httputil.ReverseProxy{
		Transport: transport,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(upstream)
			request.Out.Host = request.In.Host
			request.Out.Header.Del("Authorization")
			request.SetXForwarded()
			request.Out.Header.Set("X-Forwarded-Proto", "https")
			// DeepSeek's native root performs the cookie exchange. The browser never receives the launch token.
			if nativeToken != "" && request.In.Method == http.MethodGet && request.In.URL.Path == "/" && request.In.Header.Get("Cookie") == "" {
				query := request.Out.URL.Query()
				query.Set("token", nativeToken)
				request.Out.URL.RawQuery = query.Encode()
			}
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, _ error) {
			http.Error(w, "Harness preview is unavailable", http.StatusBadGateway)
		},
		ModifyResponse: func(response *http.Response) error {
			req := response.Request
			if nativeToken != "" && response.StatusCode == http.StatusUnauthorized &&
				req.Method == http.MethodGet && req.URL.Path == "/" && req.URL.Query().Get("token") == "" {
				retry := req.Clone(req.Context())
				query := retry.URL.Query()
				query.Set("token", nativeToken)
				retry.URL.RawQuery = query.Encode()
				next, err := transport.RoundTrip(retry)
				if err != nil {
					return err
				}
				response.Body.Close()
				*response = *next
			}
			if nativeToken != "" && strings.Contains(response.Header.Get("Location"), "token=") {
				return fmt.Errorf("unexpected native redirect")
			}
			response.Header.Add("Content-Security-Policy", fmt.Sprintf("frame-ancestors https://%s", parent))
			response.Header.Set("Referrer-Policy", "no-referrer")
			if cookie := response.Header.Values("Set-Cookie"); nativeToken != "" && len(cookie) > 0 {
				response.Header.Del("Set-Cookie")
				for _, value := range cookie {
					response.Header.Add("Set-Cookie", value+"; Secure")
				}
			}
			return nil
		},
	}
}
