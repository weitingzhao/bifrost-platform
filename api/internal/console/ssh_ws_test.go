package console

import (
	"crypto/ed25519"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// fakeSSHServer grants pty + shell on a session channel and then says nothing
// — an idle terminal, the case that leaked. It counts connections still open.
// With exitShell the shell ends right after it starts instead.
type fakeSSHServer struct {
	addr string
	open atomic.Int32
}

func startFakeSSHServer(t *testing.T, exitShell bool) *fakeSSHServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	srv := &fakeSSHServer{addr: ln.Addr().String()}
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serve(nc, cfg, exitShell)
		}
	}()
	return srv
}

func (s *fakeSSHServer) serve(nc net.Conn, cfg *ssh.ServerConfig, exitShell bool) {
	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		_ = nc.Close()
		return
	}
	s.open.Add(1)
	defer s.open.Add(-1)
	go ssh.DiscardRequests(reqs)
	go func() {
		for nch := range chans {
			if nch.ChannelType() != "session" {
				_ = nch.Reject(ssh.UnknownChannelType, "session only")
				continue
			}
			ch, chReqs, err := nch.Accept()
			if err != nil {
				continue
			}
			go func() {
				for req := range chReqs {
					ok := req.Type == "pty-req" || req.Type == "shell" || req.Type == "window-change"
					if req.WantReply {
						_ = req.Reply(ok, nil)
					}
					if req.Type == "shell" && exitShell {
						_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						_ = ch.Close()
					}
				}
			}()
		}
	}()
	_ = conn.Wait()
}

// bridgeServer is HandleWebSocket without the host allowlist: upgrade, dial
// the fake host, pipe.
func bridgeServer(t *testing.T, sshAddr string) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		client, err := ssh.Dial("tcp", sshAddr, &ssh.ClientConfig{
			User:            "console-test",
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
		if err != nil {
			t.Errorf("dial fake ssh: %v", err)
			return
		}
		defer func() { _ = client.Close() }()
		pipeShell(ws, client)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func dialBridge(t *testing.T, ts *httptest.Server) *websocket.Conn {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return ws
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A browser reload must free the host's sshd slot. The idle shell never writes,
// so this only passes if the bridge closes the SSH side itself; before the fix
// each page load leaked one connection until platform-api restarted, and a
// Mini's sshd (launchd, Instances=42) then reset every new SSH to it.
func TestPipeShellClosesSSHWhenBrowserLeaves(t *testing.T) {
	srv := startFakeSSHServer(t, false)
	ts := bridgeServer(t, srv.addr)

	ws := dialBridge(t, ts)
	eventually(t, "the SSH connection to open", func() bool { return srv.open.Load() == 1 })

	_ = ws.Close() // the tab reloads
	eventually(t, "the SSH connection to close", func() bool { return srv.open.Load() == 0 })
}

// When the shell ends (exit, logout) the browser must see the socket close,
// not a terminal that silently stops answering.
func TestPipeShellClosesSocketWhenShellExits(t *testing.T) {
	srv := startFakeSSHServer(t, true)
	ts := bridgeServer(t, srv.addr)

	ws := dialBridge(t, ts)
	defer func() { _ = ws.Close() }()
	_ = ws.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				t.Fatal("socket still open 3s after the shell exited")
			}
			break
		}
	}
	eventually(t, "the SSH connection to close", func() bool { return srv.open.Load() == 0 })
}
