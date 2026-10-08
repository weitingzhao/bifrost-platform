package releasepolicy

import (
	"bytes"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// Namespaces passed to ssh-keygen -Y sign -n. A signature made for one cannot
// be replayed as the other.
const (
	NamespacePolicy   = "bifrost-release-policy"
	NamespaceUnfreeze = "bifrost-release-unfreeze"
)

const sshsigMagic = "SSHSIG"

// VerifySSHSig checks an armored signature from `ssh-keygen -Y sign` over
// message in namespace and returns the key that made it. The caller decides
// whether that key is trusted.
//
// Format: PROTOCOL.sshsig in the OpenSSH source. The signed blob is
// "SSHSIG" || string(namespace) || string(reserved) || string(hash_alg) || string(H(message)).
func VerifySSHSig(armored, message []byte, namespace string) (ssh.PublicKey, error) {
	block, _ := pem.Decode(bytes.TrimSpace(armored))
	if block == nil || block.Type != "SSH SIGNATURE" {
		return nil, errors.New("not an armored SSH signature")
	}
	blob := block.Bytes
	if !bytes.HasPrefix(blob, []byte(sshsigMagic)) {
		return nil, errors.New("signature has no SSHSIG magic")
	}
	var sig struct {
		Version       uint32
		PublicKey     []byte
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Signature     []byte
	}
	if err := ssh.Unmarshal(blob[len(sshsigMagic):], &sig); err != nil {
		return nil, fmt.Errorf("signature blob: %w", err)
	}
	if sig.Version != 1 {
		return nil, fmt.Errorf("signature version %d is not 1", sig.Version)
	}
	if sig.Namespace != namespace {
		return nil, fmt.Errorf("signature namespace %q is not %q", sig.Namespace, namespace)
	}
	var digest []byte
	switch sig.HashAlgorithm {
	case "sha512":
		h := sha512.Sum512(message)
		digest = h[:]
	case "sha256":
		h := sha256.Sum256(message)
		digest = h[:]
	default:
		return nil, fmt.Errorf("signature hash %q is not sha256 or sha512", sig.HashAlgorithm)
	}
	pub, err := ssh.ParsePublicKey(sig.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("signature public key: %w", err)
	}
	var inner struct {
		Format string
		Blob   []byte
		Rest   []byte `ssh:"rest"`
	}
	if err := ssh.Unmarshal(sig.Signature, &inner); err != nil {
		return nil, fmt.Errorf("signature body: %w", err)
	}
	signed := append([]byte(sshsigMagic), ssh.Marshal(struct {
		Namespace     string
		Reserved      string
		HashAlgorithm string
		Hash          []byte
	}{namespace, sig.Reserved, sig.HashAlgorithm, digest})...)
	if err := pub.Verify(signed, &ssh.Signature{Format: inner.Format, Blob: inner.Blob, Rest: inner.Rest}); err != nil {
		return nil, fmt.Errorf("signature does not verify: %w", err)
	}
	return pub, nil
}

// SignerKeys returns the keys listed in an allowed_signers file
// (`principals [options] keytype base64 [comment]`), skipping comments.
func SignerKeys(allowedSigners string) []ssh.PublicKey {
	var out []ssh.PublicKey
	for _, line := range strings.Split(allowedSigners, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		for i := 1; i < len(fields); i++ {
			if pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(strings.Join(fields[i:], " "))); err == nil {
				out = append(out, pub)
				break
			}
		}
	}
	return out
}

// verifyAnchored checks the signature and that the key that made it has the
// compiled-in fingerprint.
func verifyAnchored(armored, message []byte, namespace, anchor string) error {
	if strings.TrimSpace(anchor) == "" {
		return errors.New("no trust anchor compiled into platform-api (releasepolicy.OwnerKeyFingerprint is empty)")
	}
	pub, err := VerifySSHSig(armored, message, namespace)
	if err != nil {
		return err
	}
	if fp := ssh.FingerprintSHA256(pub); fp != anchor {
		return fmt.Errorf("signed by %s, not the compiled-in Owner key %s", fp, anchor)
	}
	return nil
}
