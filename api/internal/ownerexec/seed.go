package ownerexec

import (
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// TestSeedEnv turns on the STG test seed when set to TestSeedValue. It is the
// only switch: no flag, no config file.
const (
	TestSeedEnv   = "OWNER_EXEC_TEST_SEED"
	TestSeedValue = "generate"

	// DefaultTokenFile is where Kubernetes mounts a Pod's cluster identity.
	DefaultTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// SeedConfig says where the seed comes from.
type SeedConfig struct {
	// SeedFile holds the base32 seed (a mounted Secret key). Required unless TestMode.
	SeedFile string
	// TestMode generates a throwaway seed and prints it to the log.
	TestMode bool
	// TokenFile is the cluster identity mount checked in test mode.
	TokenFile string
	// Issuer and Account label the entry in the authenticator app.
	Issuer, Account string
}

// LoadSeed returns the TOTP seed.
//
// Test mode exists so STG can be exercised without a Secret: the process makes
// its own seed and logs it, which is safe only when the process can do nothing
// with the cluster. It therefore refuses to start when a cluster identity is
// mounted or a real seed file is present, so it cannot be switched on where
// the runner has power.
func LoadSeed(cfg SeedConfig, logf func(format string, args ...any)) ([]byte, error) {
	if !cfg.TestMode {
		if strings.TrimSpace(cfg.SeedFile) == "" {
			return nil, errors.New("seed file is not configured")
		}
		raw, err := os.ReadFile(cfg.SeedFile)
		if err != nil {
			return nil, fmt.Errorf("read seed file: %w", err)
		}
		return DecodeSeed(string(raw))
	}
	if exists(cfg.TokenFile) {
		return nil, fmt.Errorf("%s=%s refused: a cluster identity is mounted at %s", TestSeedEnv, TestSeedValue, cfg.TokenFile)
	}
	if strings.TrimSpace(cfg.SeedFile) != "" && exists(cfg.SeedFile) {
		return nil, fmt.Errorf("%s=%s refused: a seed is mounted at %s", TestSeedEnv, TestSeedValue, cfg.SeedFile)
	}
	seed := make([]byte, 20)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("generate test seed: %w", err)
	}
	logf("TEST SEED (throwaway, this process has no cluster identity): %s", OTPAuthURI(seed, cfg.Issuer, cfg.Account))
	return seed, nil
}

// OTPAuthURI is the enrolment link an authenticator app opens or scans.
func OTPAuthURI(seed []byte, issuer, account string) string {
	if issuer == "" {
		issuer = "Bifrost"
	}
	if account == "" {
		account = "owner-exec"
	}
	q := url.Values{}
	q.Set("secret", EncodeSeed(seed))
	q.Set("issuer", issuer)
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + q.Encode()
}

func exists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
