package harnesssession

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

const grantLifetime = 30 * time.Second
const clockSkew = 5 * time.Second

var invalidGrant = errors.New("invalid harness grant")
var encoding = base64.RawURLEncoding

type Claims struct {
	App     string `json:"app"`
	Nonce   string `json:"nonce"`
	Expires int64  `json:"exp"`
}

func Supported(app string) bool {
	return app == "opencode" || app == "deepseek"
}

func issue(key ed25519.PrivateKey, app string, now time.Time) (string, error) {
	if !Supported(app) || len(key) != ed25519.PrivateKeySize {
		return "", invalidGrant
	}
	nonce := make([]byte, 18)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload, err := json.Marshal(Claims{App: app, Nonce: encoding.EncodeToString(nonce), Expires: now.Add(grantLifetime).Unix()})
	if err != nil {
		return "", err
	}
	return encoding.EncodeToString(payload) + "." + encoding.EncodeToString(ed25519.Sign(key, signingMessage(payload))), nil
}

func Verify(key ed25519.PublicKey, grant, app string, now time.Time) (Claims, error) {
	if len(key) != ed25519.PublicKeySize || len(grant) > 512 || !Supported(app) {
		return Claims{}, invalidGrant
	}
	parts, signature, ok := strings.Cut(grant, ".")
	if !ok {
		return Claims{}, invalidGrant
	}
	payload, err := encoding.DecodeString(parts)
	if err != nil {
		return Claims{}, invalidGrant
	}
	sig, err := encoding.DecodeString(signature)
	if err != nil || !ed25519.Verify(key, signingMessage(payload), sig) {
		return Claims{}, invalidGrant
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil || claims.App != app || len(claims.Nonce) != 24 ||
		claims.Expires <= now.Unix() || claims.Expires > now.Add(grantLifetime+clockSkew).Unix() {
		return Claims{}, invalidGrant
	}
	return claims, nil
}

func signingMessage(payload []byte) []byte {
	return append([]byte("bifrost-harness-grant/v1\x00"), payload...)
}

func LoadSigner(path string) (ed25519.PrivateKey, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	raw, err := ssh.ParseRawPrivateKey(pem)
	if err != nil {
		return nil, err
	}
	key, ok := raw.(*ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("harness signing key must be Ed25519")
	}
	return *key, nil
}

func LoadVerifier(path string) (ed25519.PublicKey, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey(file)
	if err != nil {
		return nil, err
	}
	cryptoKey, ok := key.(ssh.CryptoPublicKey)
	if !ok {
		return nil, fmt.Errorf("invalid harness public key")
	}
	verify, ok := cryptoKey.CryptoPublicKey().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("harness public key must be Ed25519")
	}
	return verify, nil
}
