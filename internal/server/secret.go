package server

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"os"
	"path/filepath"
)

// secretKey is a per-installation 32-byte key kept in data-dir/secret.key
// (0600). It encrypts secrets at rest (the SMTP password) so they are not
// stored in the database in clear text.
func loadSecretKey(dataDir string) []byte {
	path := filepath.Join(dataDir, "secret.key")
	if b, err := os.ReadFile(path); err == nil && len(b) == 32 {
		return b
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil
	}
	_ = os.WriteFile(path, key, 0o600)
	return key
}

// encryptSecret returns base64(nonce||ciphertext); "" stays "".
func (s *Server) encryptSecret(plain string) string {
	if plain == "" || len(s.secret) != 32 {
		return ""
	}
	block, err := aes.NewCipher(s.secret)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ""
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(out)
}

func (s *Server) decryptSecret(enc string) string {
	if enc == "" || len(s.secret) != 32 {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(s.secret)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return ""
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return ""
	}
	return string(plain)
}
