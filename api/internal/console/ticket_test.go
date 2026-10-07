package console

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

func TestTicketIsOneUseForOneHostAndExpires(t *testing.T) {
	s := newTicketStore()
	now := time.Date(2026, 10, 7, 1, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	id, err := s.issue("node-a", "operator")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.consume(id, "node-b"); ok {
		t.Fatal("a ticket for node-a opened node-b")
	}
	if _, ok := s.consume(id, "node-a"); ok {
		t.Fatal("a ticket spent on a wrong host must not work afterwards")
	}

	id, _ = s.issue("node-a", "operator")
	if who, ok := s.consume(id, "node-a"); !ok || who != "operator" {
		t.Fatalf("fresh ticket refused (who=%q ok=%v)", who, ok)
	}
	if _, ok := s.consume(id, "node-a"); ok {
		t.Fatal("a ticket worked twice")
	}

	id, _ = s.issue("node-a", "operator")
	now = now.Add(ticketTTL + time.Second)
	if _, ok := s.consume(id, "node-a"); ok {
		t.Fatal("an expired ticket worked")
	}
	if _, ok := s.consume("", "node-a"); ok {
		t.Fatal("an empty ticket worked")
	}
}

func testHandler() *Handler {
	h := NewHandler(&config.Config{Topology: &config.TopologyFile{
		Nodes: []config.TopologyNode{{ID: "node-a", Label: "Node A", Host: "10.0.0.1", Group: "linux"}},
	}})
	return h
}

// Before 2026-10-07 anyone who could reach the port got a shell (TD-203): the
// handler must refuse before upgrading or dialing anything.
func TestWebSocketWithoutTicketIsRefusedBeforeUpgrade(t *testing.T) {
	h := testHandler()
	for _, q := range []string{"node=node-a", "node=node-a&ticket=nope"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/console/ws?"+q, nil)
		req.Header.Set("Origin", "http://127.0.0.1:5180")
		h.HandleWebSocket(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status %d, want 401", q, rec.Code)
		}
	}
}

func TestUpgraderRefusesMissingOrForeignOrigin(t *testing.T) {
	for origin, want := range map[string]bool{"": false, "http://evil.lan": false, "http://127.0.0.1:5180": true} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		if got := upgrader.CheckOrigin(req); got != want {
			t.Fatalf("origin %q: allowed=%v, want %v", origin, got, want)
		}
	}
}

func TestHostKeysAreCheckedAgainstKnownHosts(t *testing.T) {
	srv := startFakeSSHServer(t, true)
	dir := t.TempDir()
	dial := func(knownHostsBody string) error {
		path := filepath.Join(dir, "known_hosts")
		if err := os.WriteFile(path, []byte(knownHostsBody), 0o600); err != nil {
			t.Fatal(err)
		}
		cb, err := hostKeyCallback(SSHSettings{KnownHosts: path})
		if err != nil {
			return err
		}
		c, err := ssh.Dial("tcp", srv.addr, &ssh.ClientConfig{User: "t", HostKeyCallback: cb, Timeout: 5 * time.Second})
		if err == nil {
			_ = c.Close()
		}
		return err
	}

	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	other, _ := ssh.NewPublicKey(pub)
	if err := dial(knownhosts.Line([]string{srv.addr}, other) + "\n"); err == nil {
		t.Fatal("a host presenting a key other than the known one was accepted")
	}
	if err := dial(""); err == nil {
		t.Fatal("a host missing from known_hosts was accepted")
	}
	if _, err := hostKeyCallback(SSHSettings{KnownHosts: filepath.Join(dir, "absent")}); err == nil {
		t.Fatal("a missing known_hosts file must refuse every host")
	}
	// the fake server's own key, learned from its first handshake, is accepted
	var seen ssh.PublicKey
	c, err := ssh.Dial("tcp", srv.addr, &ssh.ClientConfig{User: "t", Timeout: 5 * time.Second,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error { seen = key; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if err := dial(knownhosts.Line([]string{srv.addr}, seen) + "\n"); err != nil {
		t.Fatalf("the known key was refused: %v", err)
	}
}

// 10-07: .50 offers ecdsa, ed25519 and rsa while known_hosts holds only its
// ed25519 key; Go negotiated ecdsa and reported a mismatch for a known host.
func TestHandshakeAsksForAKeyTypeKnownHostsHas(t *testing.T) {
	_, edKey, _ := ed25519.GenerateKey(rand.Reader)
	edSigner, _ := ssh.NewSignerFromKey(edKey)
	ecKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	ecSigner, _ := ssh.NewSignerFromKey(ecKey)
	cfg := &ssh.ServerConfig{NoClientAuth: true}
	cfg.AddHostKey(ecSigner)
	cfg.AddHostKey(edSigner)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				c, chans, reqs, err := ssh.NewServerConn(nc, cfg)
				if err != nil {
					return
				}
				go ssh.DiscardRequests(reqs)
				go func() {
					for range chans {
					}
				}()
				_ = c.Wait()
			}()
		}
	}()
	addr := ln.Addr().String()
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := os.WriteFile(path, []byte(knownhosts.Line([]string{addr}, edSigner.PublicKey())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cb, err := hostKeyCallback(SSHSettings{KnownHosts: path})
	if err != nil {
		t.Fatal(err)
	}
	c, err := ssh.Dial("tcp", addr, &ssh.ClientConfig{User: "t", HostKeyCallback: cb,
		HostKeyAlgorithms: hostKeyAlgorithms(cb, addr), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("known ed25519 host refused: %v", err)
	}
	_ = c.Close()
}
