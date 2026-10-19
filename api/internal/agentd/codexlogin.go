package agentd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// codex's login and an attempt's own codex home (TD-371). Each attempt's
// codex runs with a CODEX_HOME of its own that holds only the host's
// auth.json, copied in by agentd. On the agent host that is a ChatGPT login
// (codex login --device-auth), whose refresh token is rotated when codex
// refreshes it: the refreshed login is in the attempt's copy, and the host's
// would be spent. So once the executor is gone agentd takes the refreshed
// tokens back, and only those: from a file that says it is the same login
// (its mode, its API key and its account id the host's) refreshed later than
// the host's. The executor can write that file and agentd cannot check a
// token's signature, so a run can spoil the host's login (it then has to log
// in again); it cannot put more than these tokens there. One codex attempt
// runs at a time on a host (Run), so a login is not copied while another
// attempt may be refreshing it (review 300 B5).

// maxCodexLogin is the most of an auth.json agentd reads.
const maxCodexLogin = 64 << 10

// codexLoginMu: one write-back at a time in this process.
var codexLoginMu sync.Mutex

// CodexHome is the host user's codex home in environ: CODEX_HOME, else
// $HOME/.codex, else "" (no login is copied).
func CodexHome(environ []string) string {
	if v := envValues(environ, "CODEX_HOME"); len(v) > 0 && v[len(v)-1] != "" {
		return v[len(v)-1]
	}
	if v := envValues(environ, "HOME"); len(v) > 0 && v[len(v)-1] != "" {
		return filepath.Join(v[len(v)-1], ".codex")
	}
	return ""
}

type codexTokens struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	AccountID    string `json:"account_id"`
}

type codexLogin struct {
	AuthMode    string       `json:"auth_mode"`
	APIKey      *string      `json:"OPENAI_API_KEY"`
	Tokens      *codexTokens `json:"tokens"`
	LastRefresh string       `json:"last_refresh"`
}

// readLogin reads an auth.json: a regular file, not a link, at most
// maxCodexLogin bytes.
func readLogin(path string) ([]byte, error) {
	f, err := openBounded(path, maxCodexLogin+1, false)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(f)
	if err == nil && len(body) > maxCodexLogin {
		err = fmt.Errorf("agentd: %s is over %d bytes", path, maxCodexLogin)
	}
	return body, err
}

// readLoginIn is readLogin for rel inside r: a regular file, the one looked
// at, at most maxCodexLogin bytes.
func readLoginIn(r *os.Root, rel string) ([]byte, error) {
	fi, err := r.Lstat(rel)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("agentd: %s is not a regular file", rel)
	}
	f, err := r.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	if st, serr := f.Stat(); serr != nil || !os.SameFile(st, fi) {
		return nil, fmt.Errorf("agentd: %s changed while it was opened", rel)
	}
	body, err := io.ReadAll(io.LimitReader(f, maxCodexLogin+1))
	if err == nil && len(body) > maxCodexLogin {
		err = fmt.Errorf("agentd: %s is over %d bytes", rel, maxCodexLogin)
	}
	return body, err
}

// copyCodexLogin puts the host's auth.json, when there is one, into home, an
// attempt's new codex home.
func copyCodexLogin(master, home string) error {
	if master == "" {
		return nil
	}
	body, err := readLogin(filepath.Join(master, "auth.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(home, "auth.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return err
	}
	if _, err = f.Write(body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// token: what a token may be, printable ASCII of a sane length.
func token(s string) bool {
	if s == "" || len(s) > 16<<10 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func sameKey(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// refreshed says why got, an attempt's auth.json, is not a later refresh of
// cur, the host's ("" when it is); now bounds how far ahead it may claim to be.
func refreshed(cur, got codexLogin, now time.Time) string {
	switch {
	case got.Tokens == nil || cur.Tokens == nil:
		return "no tokens"
	case !token(got.Tokens.IDToken) || !token(got.Tokens.AccessToken) || !token(got.Tokens.RefreshToken) || !token(got.Tokens.AccountID):
		return "a token is not one"
	case got.AuthMode != cur.AuthMode || !sameKey(got.APIKey, cur.APIKey):
		return "another kind of login"
	case got.Tokens.AccountID != cur.Tokens.AccountID:
		return "another account"
	}
	t1, err1 := time.Parse(time.RFC3339Nano, got.LastRefresh)
	t0, err0 := time.Parse(time.RFC3339Nano, cur.LastRefresh)
	switch {
	case err1 != nil:
		return "no time of refresh"
	case err0 == nil && !t1.After(t0):
		return "not refreshed since"
	case t1.After(now.Add(5 * time.Minute)):
		return "refreshed in the future"
	}
	return ""
}

// keepCodexLogin takes a refreshed login in out, an attempt's output
// opened as a root (openWithin), back into master's auth.json: only its
// tokens and time of refresh, into the host's file as it is, replaced at
// once. Nothing is done when the attempt's login is the host's or there is
// none; an error says why it was not taken.
func keepCodexLogin(master string, out *os.Root) error {
	codexLoginMu.Lock()
	defer codexLoginMu.Unlock()
	body, err := readLoginIn(out, filepath.Join("home", ".codex", "auth.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	path := filepath.Join(master, "auth.json")
	curBody, err := readLogin(path)
	if err != nil {
		return err
	}
	if bytes.Equal(body, curBody) {
		return nil
	}
	var got, cur codexLogin
	if err = json.Unmarshal(body, &got); err != nil {
		return fmt.Errorf("the attempt's auth.json: %w", err)
	}
	if err = json.Unmarshal(curBody, &cur); err != nil {
		return fmt.Errorf("the host's auth.json: %w", err)
	}
	if why := refreshed(cur, got, time.Now()); why != "" {
		if why == "not refreshed since" {
			return nil
		}
		return errors.New(why)
	}
	// The host's file with the attempt's tokens and time put in: whatever
	// else either holds stays the host's.
	var doc, toks map[string]json.RawMessage
	if err = json.Unmarshal(curBody, &doc); err != nil {
		return err
	}
	if err = json.Unmarshal(doc["tokens"], &toks); err != nil {
		return err
	}
	for k, v := range map[string]string{"id_token": got.Tokens.IDToken, "access_token": got.Tokens.AccessToken,
		"refresh_token": got.Tokens.RefreshToken, "account_id": got.Tokens.AccountID} {
		toks[k], _ = json.Marshal(v)
	}
	doc["tokens"], _ = json.Marshal(toks)
	doc["last_refresh"], _ = json.Marshal(got.LastRefresh)
	merged, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return replaceFile(path, merged)
}

// replaceFile writes body to path at once: a file beside it, synced, renamed
// over it, owner only.
func replaceFile(path string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".auth.json.*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err = tmp.Chmod(0o600); err == nil {
		if _, err = tmp.Write(body); err == nil {
			err = tmp.Sync()
		}
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
