package application

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	fulfillmentdomain "github.com/dujiao-next/internal/modules/fulfillment/domain"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	"github.com/dujiao-next/internal/shared/money"

	"github.com/shopspring/decimal"
)

func TestSyncFulfilledPostsPayloadWithSecret(t *testing.T) {
	var gotSecret string
	var gotPayload syncPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Dujiao-Sync-Secret")
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Fatalf("decode request body failed: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	svc := New(config.TeamGenieConfig{
		Enabled:        true,
		WebhookURL:     server.URL,
		SharedSecret:   "secret-value",
		Channel:        "main",
		SourcePlatform: "dujiao-next",
		SourceSite:     "moshshop",
		TimeoutMS:      1000,
	}, nil, nil)

	order := &orderdomain.Order{
		ID:          12,
		OrderNo:     "DJ202607280001",
		GuestEmail:  "guest@example.com",
		TotalAmount: money.FromDecimal(decimal.RequireFromString("19.90")),
		Currency:    "CNY",
		Fulfillment: &fulfillmentdomain.Fulfillment{
			ID:        31,
			Status:    "delivered",
			Payload:   "card-one\n\ncard-two\r\n",
			CreatedAt: time.Now(),
		},
	}

	if err := svc.SyncFulfilled(order); err != nil {
		t.Fatalf("sync fulfilled failed: %v", err)
	}
	if gotSecret != "secret-value" {
		t.Fatalf("unexpected sync secret: %q", gotSecret)
	}
	if gotPayload.OrderNo != order.OrderNo || gotPayload.BuyerEmail != order.GuestEmail {
		t.Fatalf("unexpected payload identity: %+v", gotPayload)
	}
	if gotPayload.Price != "19.90" || gotPayload.Currency != "CNY" {
		t.Fatalf("unexpected payload money: %+v", gotPayload)
	}
	if gotPayload.Channel != "main" || gotPayload.SourcePlatform != "dujiao-next" || gotPayload.SourceSite != "moshshop" {
		t.Fatalf("unexpected payload source metadata: %+v", gotPayload)
	}
	if len(gotPayload.Fulfillment.Cards) != 2 || gotPayload.Fulfillment.Cards[0] != "card-one" || gotPayload.Fulfillment.Cards[1] != "card-two" {
		t.Fatalf("unexpected payload cards: %+v", gotPayload.Fulfillment.Cards)
	}
}
