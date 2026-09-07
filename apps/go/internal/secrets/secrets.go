// Package secrets encrypts configuration values that must not sit in the database
// in clear text — integration tokens, webhook URLs, authorization headers — and
// decides which configuration keys count as secret.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Prefix marks an encrypted value so plain text written by an older version can
// still be read, and so a value is never double-encrypted.
const Prefix = "enc:v1:"

// Mask is what the API returns in place of a secret. A client that sends a value
// starting with it back is asking to keep the stored secret unchanged.
const Mask = "********"

var (
	// ErrNoKey is returned when encryption is requested but no key is configured.
	ErrNoKey = errors.New("SECRETS_ENCRYPTION_KEY is not configured")
	// ErrKeySize is returned for a key that does not decode to 32 bytes.
	ErrKeySize = errors.New("SECRETS_ENCRYPTION_KEY must be base64 of 32 bytes")
)

// Cipher encrypts and decrypts with AES-256-GCM. A nil *Cipher is valid and acts
// as a pass-through, so callers never branch on configuration.
type Cipher struct {
	aead cipher.AEAD
}

// New builds a Cipher from a base64-encoded 32-byte key. An empty key yields a
// nil Cipher: values are stored as they are, and Enabled reports false.
func New(base64Key string) (*Cipher, error) {
	base64Key = strings.TrimSpace(base64Key)
	if base64Key == "" {
		return nil, nil
	}
	raw, err := base64.StdEncoding.DecodeString(base64Key)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(base64Key)
	}
	if err != nil {
		return nil, fmt.Errorf("decode SECRETS_ENCRYPTION_KEY: %w", err)
	}
	if len(raw) != 32 {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Enabled reports whether values will actually be encrypted.
func (c *Cipher) Enabled() bool { return c != nil && c.aead != nil }

// Encrypt returns the value in transport form. Without a key, or for a value that
// is already encrypted, the input is returned unchanged.
func (c *Cipher) Encrypt(plain string) (string, error) {
	if !c.Enabled() || plain == "" || IsEncrypted(plain) {
		return plain, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, []byte(plain), nil)
	return Prefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt returns the clear value. A value without the prefix is returned as it
// is, so data written before encryption was enabled keeps working.
func (c *Cipher) Decrypt(value string) (string, error) {
	if !IsEncrypted(value) {
		return value, nil
	}
	if !c.Enabled() {
		return "", ErrNoKey
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, Prefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted value: %w", err)
	}
	size := c.aead.NonceSize()
	if len(raw) < size {
		return "", errors.New("encrypted value is truncated")
	}
	plain, err := c.aead.Open(nil, raw[:size], raw[size:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt value: %w", err)
	}
	return string(plain), nil
}

// IsEncrypted reports whether the value carries the encryption prefix.
func IsEncrypted(value string) bool { return strings.HasPrefix(value, Prefix) }

// IsMasked reports whether a value is the API mask rather than a real secret.
func IsMasked(value string) bool { return strings.HasPrefix(value, Mask) }

// MaskValue renders a secret for the API: the mask plus the last four characters,
// enough to recognise which key is configured without revealing it.
func MaskValue(plain string) string {
	if plain == "" {
		return ""
	}
	if len(plain) <= 4 {
		return Mask
	}
	return Mask + plain[len(plain)-4:]
}

// secretKeyMarkers are the name fragments that identify a secret configuration
// key across integrations: bot tokens, webhook URLs, routing keys, auth headers.
var secretKeyMarkers = []string{
	"token", "secret", "password", "passwd", "apikey", "api_key",
	"webhook", "routingkey", "routing_key", "authorization", "headers",
	"privatekey", "private_key", "credential",
}

// IsSecretKey reports whether a configuration key holds a secret, by name.
func IsSecretKey(key string) bool {
	lowered := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range secretKeyMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// EncryptConfig encrypts every secret-valued string in a configuration map.
func (c *Cipher) EncryptConfig(config map[string]any) (map[string]any, error) {
	return c.mapSecrets(config, c.Encrypt)
}

// DecryptConfig decrypts every secret-valued string in a configuration map.
func (c *Cipher) DecryptConfig(config map[string]any) (map[string]any, error) {
	return c.mapSecrets(config, c.Decrypt)
}

// MaskConfig replaces every secret value with its masked form for API responses.
func MaskConfig(config map[string]any) map[string]any {
	out, _ := (*Cipher)(nil).mapSecrets(config, func(v string) (string, error) { return MaskValue(v), nil })
	return out
}

// MergeMasked keeps the stored secret wherever the incoming configuration still
// carries the mask, so a client can re-save a form without knowing the secret.
func MergeMasked(incoming, stored map[string]any) map[string]any {
	if incoming == nil {
		return nil
	}
	out := make(map[string]any, len(incoming))
	for key, value := range incoming {
		text, isString := value.(string)
		if isString && IsSecretKey(key) && IsMasked(text) {
			if previous, ok := stored[key]; ok {
				out[key] = previous
				continue
			}
			// Nothing stored behind the mask: drop it rather than store the mask.
			continue
		}
		out[key] = value
	}
	return out
}

func (c *Cipher) mapSecrets(config map[string]any, fn func(string) (string, error)) (map[string]any, error) {
	if config == nil {
		return nil, nil
	}
	out := make(map[string]any, len(config))
	for key, value := range config {
		text, isString := value.(string)
		if !isString || !IsSecretKey(key) || text == "" {
			out[key] = value
			continue
		}
		mapped, err := fn(text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		out[key] = mapped
	}
	return out, nil
}
