package console

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/weitingzhao/bifrost-platform/api/internal/config"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  4096,
	WriteBufferSize: 4096,
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true
		}
		for _, a := range []string{"http://127.0.0.1:5180", "http://localhost:5180"} {
			if origin == a {
				return true
			}
		}
		return false
	},
}

type SSHSettings struct {
	User           string
	KeyPath        string
	AllowLocalhost bool
	DialTimeout    time.Duration
}

func SSHSettingsFromEnv() SSHSettings {
	user := os.Getenv("PLATFORM_SSH_USER")
	if user == "" {
		user = "vision"
	}
	return SSHSettings{
		User:           user,
		KeyPath:        strings.TrimSpace(os.Getenv("PLATFORM_SSH_KEY_PATH")),
		AllowLocalhost: os.Getenv("PLATFORM_SSH_ALLOW_LOCALHOST") == "1",
		DialTimeout:    15 * time.Second,
	}
}

type resizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

type Handler struct {
	cfg *config.Config
	ssh SSHSettings
}

func NewHandler(cfg *config.Config) *Handler {
	return &Handler{cfg: cfg, ssh: SSHSettingsFromEnv()}
}

func (h *Handler) HandleHosts(w http.ResponseWriter, _ *http.Request) {
	hosts := ListHosts(h.cfg.Topology, h.ssh.User, h.ssh.AllowLocalhost)
	type row struct {
		Host
		Reachable bool `json:"reachable"`
	}
	out := make([]row, 0, len(hosts))
	for _, host := range hosts {
		out = append(out, row{
			Host:      host,
			Reachable: Reachable(host.Host, host.Port, 2*time.Second),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"hosts": out})
}

func (h *Handler) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	if h.cfg.Topology == nil {
		http.Error(w, "topology not loaded", http.StatusInternalServerError)
		return
	}

	nodeID := r.URL.Query().Get("node")
	hostQ := r.URL.Query().Get("host")
	target, ok := FindHost(h.cfg.Topology, nodeID, hostQ, h.ssh.User, h.ssh.AllowLocalhost)
	if !ok {
		http.Error(w, "host not in allowlist", http.StatusForbidden)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("console ws upgrade: %v", err)
		return
	}
	defer ws.Close()

	client, agentConn, err := dialSSH(target, h.ssh)
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31mSSH connect failed: %v\x1b[0m\r\n", err)))
		return
	}
	defer client.Close()
	if agentConn != nil {
		defer agentConn.Close()
	}

	session, err := client.NewSession()
	if err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31mSSH session: %v\x1b[0m\r\n", err)))
		return
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return
	}

	if err := session.RequestPty("xterm-256color", 32, 120, terminalModes()); err != nil {
		_ = ws.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("\r\n\x1b[31mPTY: %v\x1b[0m\r\n", err)))
		return
	}
	if err := session.Shell(); err != nil {
		return
	}

	var wg sync.WaitGroup
	done := make(chan struct{})

	pump := func(r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump(stdout)
	go pump(stderr)

	go func() {
		defer close(done)
		for {
			mt, data, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if mt == websocket.TextMessage {
				var msg resizeMsg
				if json.Unmarshal(data, &msg) == nil && msg.Type == "resize" && msg.Cols > 0 && msg.Rows > 0 {
					_ = session.WindowChange(msg.Rows, msg.Cols)
				}
				continue
			}
			if mt == websocket.BinaryMessage && len(data) > 0 {
				if _, err := stdin.Write(data); err != nil {
					return
				}
			}
		}
	}()

	wg.Wait()
	<-done
}

func dialSSH(target Host, settings SSHSettings) (*ssh.Client, net.Conn, error) {
	auth, agentConn, err := buildSSHAuth(settings.KeyPath)
	if err != nil {
		return nil, nil, err
	}
	addr := net.JoinHostPort(target.Host, itoa(target.Port))
	cfg := &ssh.ClientConfig{
		User:            target.User,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         settings.DialTimeout,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		if agentConn != nil {
			_ = agentConn.Close()
		}
		return nil, nil, err
	}
	return client, agentConn, nil
}

// buildSSHAuth returns auth methods. When using ssh-agent, agentConn must stay
// open until the SSH client is closed (signers call back into the agent socket).
func buildSSHAuth(keyPath string) ([]ssh.AuthMethod, net.Conn, error) {
	if keyPath != "" {
		signer, err := loadSigner(keyPath)
		if err != nil {
			return nil, nil, err
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil
	}
	for _, p := range defaultKeyPaths() {
		signer, err := loadSigner(p)
		if err == nil {
			return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil, nil
		}
	}
	if signers, conn, err := agentSigners(); err == nil && len(signers) > 0 {
		return []ssh.AuthMethod{ssh.PublicKeys(signers...)}, conn, nil
	}
	return nil, nil, fmt.Errorf(
		"no usable SSH key (add key to ssh-agent with ssh-add, set PLATFORM_SSH_KEY_PATH to an unencrypted deploy key, or unlock ~/.ssh/id_ed25519)",
	)
}

func defaultKeyPaths() []string {
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	if home == "" {
		return nil
	}
	names := []string{"id_ed25519", "id_rsa", "id_ecdsa"}
	paths := make([]string, 0, len(names))
	for _, name := range names {
		paths = append(paths, filepath.Join(home, ".ssh", name))
	}
	return paths
}

func agentSigners() ([]ssh.Signer, net.Conn, error) {
	sock := strings.TrimSpace(os.Getenv("SSH_AUTH_SOCK"))
	if sock == "" {
		return nil, nil, fmt.Errorf("SSH_AUTH_SOCK not set")
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return nil, nil, err
	}
	signers, err := agent.NewClient(conn).Signers()
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if len(signers) == 0 {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("ssh-agent has no keys (run ssh-add)")
	}
	return signers, conn, nil
}

func loadSigner(path string) (ssh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}
	return signer, nil
}

func terminalModes() ssh.TerminalModes {
	return ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
