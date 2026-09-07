package secrets

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func testKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func TestRoundTrip(t *testing.T) {
	c, err := New(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := c.Encrypt("xoxb-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(sealed) || strings.Contains(sealed, "xoxb") {
		t.Fatalf("value not encrypted: %q", sealed)
	}
	plain, err := c.Decrypt(sealed)
	if err != nil {
		t.Fatal(err)
	}
	if plain != "xoxb-secret-token" {
		t.Fatalf("round trip = %q", plain)
	}
}

func TestNilCipherPassesThroughAndRefusesEncryptedInput(t *testing.T) {
	var c *Cipher
	if c.Enabled() {
		t.Fatal("nil cipher must not report enabled")
	}
	out, err := c.Encrypt("plain")
	if err != nil || out != "plain" {
		t.Fatalf("nil encrypt = %q, %v", out, err)
	}
	if _, err := c.Decrypt(Prefix + "abc"); err == nil {
		t.Fatal("nil cipher must refuse to decrypt an encrypted value")
	}
}

func TestNewRejectsBadKey(t *testing.T) {
	if _, err := New(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected key size error")
	}
	if c, err := New("   "); err != nil || c != nil {
		t.Fatalf("empty key should yield nil cipher, got %v, %v", c, err)
	}
}

func TestIsSecretKey(t *testing.T) {
	for _, key := range []string{"telegramBotToken", "slackWebhookUrl", "pagerdutyRoutingKey", "headers", "clientSecret", "apiKey"} {
		if !IsSecretKey(key) {
			t.Errorf("%s should be a secret key", key)
		}
	}
	for _, key := range []string{"enabled", "endpoint", "traceLinkTemplate", "dedupeWindowSeconds", "telegramChatId"} {
		if IsSecretKey(key) {
			t.Errorf("%s should not be a secret key", key)
		}
	}
}

func TestConfigEncryptDecryptMask(t *testing.T) {
	c, err := New(testKey(t))
	if err != nil {
		t.Fatal(err)
	}
	config := map[string]any{"telegramBotToken": "123:abcdef", "telegramChatId": "42", "enabled": true}

	stored, err := c.EncryptConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(stored["telegramBotToken"].(string)) || stored["telegramChatId"] != "42" || stored["enabled"] != true {
		t.Fatalf("unexpected stored config: %v", stored)
	}

	loaded, err := c.DecryptConfig(stored)
	if err != nil {
		t.Fatal(err)
	}
	if loaded["telegramBotToken"] != "123:abcdef" {
		t.Fatalf("decrypt = %v", loaded["telegramBotToken"])
	}

	masked := MaskConfig(loaded)
	if masked["telegramBotToken"] != Mask+"cdef" || masked["telegramChatId"] != "42" {
		t.Fatalf("mask = %v", masked)
	}
}

func TestMergeMaskedKeepsStoredSecret(t *testing.T) {
	stored := map[string]any{"telegramBotToken": "real", "telegramChatId": "42"}
	incoming := map[string]any{"telegramBotToken": Mask + "real", "telegramChatId": "43"}
	merged := MergeMasked(incoming, stored)
	if merged["telegramBotToken"] != "real" || merged["telegramChatId"] != "43" {
		t.Fatalf("merge = %v", merged)
	}

	// A mask with nothing behind it is dropped, never stored as a literal.
	merged = MergeMasked(map[string]any{"slackWebhookUrl": Mask}, map[string]any{})
	if _, ok := merged["slackWebhookUrl"]; ok {
		t.Fatalf("mask stored as a secret: %v", merged)
	}

	// A new real value replaces the stored one.
	merged = MergeMasked(map[string]any{"telegramBotToken": "new"}, stored)
	if merged["telegramBotToken"] != "new" {
		t.Fatalf("new secret not accepted: %v", merged)
	}
}
