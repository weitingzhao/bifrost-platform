package harnesssession

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestGrantIsShortLivedAndBoundToOneHarness(t *testing.T) {
	pub, signer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1200, 0)
	grant, err := issue(signer, "opencode", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(pub, grant, "opencode", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		grant, app string
		at         time.Time
	}{
		{grant, "deepseek", now},
		{grant, "opencode", now.Add(grantLifetime)},
		{grant + "a", "opencode", now},
	} {
		if _, err := Verify(pub, test.grant, test.app, test.at); err == nil {
			t.Fatalf("accepted unauthorized grant for %s", test.app)
		}
	}
}

func TestReadSigningKeyAndVerifier(t *testing.T) {
	pub, signer, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pp, err := ssh.MarshalPrivateKey(signer, "test")
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	private, public := filepath.Join(dir, "signer"), filepath.Join(dir, "signer.pub")
	if err := os.WriteFile(private, pem.EncodeToMemory(pp), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(public, ssh.MarshalAuthorizedKey(sshPub), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := LoadSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	verify, err := LoadVerifier(public)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	grant, err := issue(key, "deepseek", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(verify, grant, "deepseek", now.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
}
