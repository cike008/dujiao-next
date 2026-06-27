package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/models"
	"github.com/dujiao-next/internal/repository"
	"github.com/shopspring/decimal"
)

type teamGenieTestUserRepo struct {
	user *models.User
	err  error
}

func (r teamGenieTestUserRepo) GetByEmail(string) (*models.User, error) { return nil, nil }
func (r teamGenieTestUserRepo) GetByID(uint) (*models.User, error)      { return r.user, r.err }
func (r teamGenieTestUserRepo) ListByIDs([]uint) ([]models.User, error) { return nil, nil }
func (r teamGenieTestUserRepo) Create(*models.User) error               { return nil }
func (r teamGenieTestUserRepo) Update(*models.User) error               { return nil }
func (r teamGenieTestUserRepo) List(repository.UserListFilter) ([]models.User, int64, error) {
	return nil, 0, nil
}
func (r teamGenieTestUserRepo) BatchUpdateStatus([]uint, string) error { return nil }
func (r teamGenieTestUserRepo) IncrementTotalRecharged(uint, decimal.Decimal) error {
	return nil
}
func (r teamGenieTestUserRepo) IncrementTotalSpent(uint, decimal.Decimal) error {
	return nil
}
func (r teamGenieTestUserRepo) UpdateMemberLevelIfCurrent(uint, uint, uint) (int64, error) {
	return 0, nil
}
func (r teamGenieTestUserRepo) AssignDefaultMemberLevel(uint) (int64, error) {
	return 0, nil
}
func (r teamGenieTestUserRepo) UpdateTOTPPending(uint, string, time.Time) error {
	return nil
}
func (r teamGenieTestUserRepo) UpdateTOTPEnabled(uint, string, time.Time, string) error {
	return nil
}
func (r teamGenieTestUserRepo) UpdateRecoveryCodes(uint, string) error { return nil }
func (r teamGenieTestUserRepo) ClearTOTP(uint) error                   { return nil }

func TestTeamGenieSyncPayloadUsesRegisteredUserEmail(t *testing.T) {
	svc := NewTeamGenieSyncService(config.TeamGenieSyncConfig{
		Enabled:      true,
		WebhookURL:   "http://127.0.0.1/webhook",
		SharedSecret: "secret",
	}, teamGenieTestUserRepo{user: &models.User{Email: "buyer@example.com"}})

	payload, ok := svc.buildPayload(&models.Order{
		ID:          1,
		OrderNo:     "NO123",
		UserID:      42,
		TotalAmount: models.NewMoneyFromDecimal(decimal.RequireFromString("9.90")),
		Currency:    "USD",
	}, &models.Fulfillment{Payload: "CARD-1\nCARD-2", Status: "delivered"})
	if !ok {
		t.Fatalf("expected payload to be built")
	}
	if payload.BuyerEmail != "buyer@example.com" {
		t.Fatalf("buyer email want buyer@example.com got %q", payload.BuyerEmail)
	}
}

func TestTeamGenieSyncPostSendsSecretHeader(t *testing.T) {
	var gotSecret string
	var gotPayload teamGenieSyncPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Dujiao-Sync-Secret")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := NewTeamGenieSyncService(config.TeamGenieSyncConfig{
		Enabled:      true,
		WebhookURL:   server.URL,
		SharedSecret: "shared-secret",
		TimeoutMS:    1000,
	}, nil)

	err := svc.SyncFulfilled(&models.Order{
		ID:          1,
		OrderNo:     "NO123",
		GuestEmail:  "guest@example.com",
		TotalAmount: models.NewMoneyFromDecimal(decimal.RequireFromString("9.90")),
		Currency:    "USD",
	}, &models.Fulfillment{Payload: "CARD-1", Status: "delivered"})
	if err != nil {
		t.Fatalf("sync fulfilled: %v", err)
	}
	if gotSecret != "shared-secret" {
		t.Fatalf("secret header mismatch")
	}
	if gotPayload.BuyerEmail != "guest@example.com" || len(gotPayload.Fulfillment.Cards) != 1 {
		t.Fatalf("unexpected payload: %+v", gotPayload)
	}
}

func TestSanitizeWebhookURLForLog(t *testing.T) {
	got := sanitizeWebhookURLForLog("https://user:pass@example.com/webhook?token=secret#frag")
	want := "https://%2A%2A%2A@example.com/webhook"
	if got != want {
		t.Fatalf("sanitized url want %q got %q", want, got)
	}
}
