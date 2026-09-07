package repo

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"pipelogiq/internal/observability/model"
	"pipelogiq/internal/secrets"
)

// A secret written through the repository must not be readable from the row,
// and must come back in clear text through the repository.
func TestUpsertIntegrationConfig_EncryptsSecretsAtRest(t *testing.T) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	cipher, err := secrets.New(base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatal(err)
	}

	alertingType, _ := model.ParseIntegrationType("alerting")
	db := setupTestDB(t)
	repo := NewSQLRepository(db, cipher)
	ctx := context.Background()
	if err := repo.EnsureIntegrations(ctx, []model.IntegrationType{alertingType}); err != nil {
		t.Fatal(err)
	}

	config := map[string]any{"telegramBotToken": "123:verysecret", "telegramChatId": "42"}
	if err := repo.UpsertIntegrationConfig(ctx, alertingType, config, model.IntegrationStatusConfigured); err != nil {
		t.Fatal(err)
	}

	var stored string
	if err := db.Get(&stored, `SELECT config_json FROM observability_integration_config WHERE type = ?`, string(alertingType)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "verysecret") || !strings.Contains(stored, secrets.Prefix) {
		t.Fatalf("token stored in clear text: %s", stored)
	}
	if !strings.Contains(stored, `"42"`) {
		t.Fatalf("non-secret value must stay readable: %s", stored)
	}

	loaded, err := repo.GetIntegration(ctx, alertingType)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Config["telegramBotToken"] != "123:verysecret" {
		t.Fatalf("decrypted read = %v", loaded.Config["telegramBotToken"])
	}
}
