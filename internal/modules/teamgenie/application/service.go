package application

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/logger"
	usercontract "github.com/dujiao-next/internal/modules/identity/user/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	"github.com/dujiao-next/internal/queue"
)

// Service syncs fulfilled card secrets to TeamGenie after automatic delivery.
type Service struct {
	cfg        config.TeamGenieConfig
	httpClient *http.Client
	users      usercontract.Store
	queue      *queue.Client
}

type syncPayload struct {
	OrderNo        string                 `json:"order_no"`
	BuyerEmail     string                 `json:"buyer_email,omitempty"`
	Price          string                 `json:"price,omitempty"`
	Currency       string                 `json:"currency,omitempty"`
	Channel        string                 `json:"channel,omitempty"`
	SourceOrderNo  string                 `json:"source_order_no,omitempty"`
	SourcePlatform string                 `json:"source_platform,omitempty"`
	SourceSite     string                 `json:"source_site,omitempty"`
	SoldMetadata   map[string]interface{} `json:"sold_metadata,omitempty"`
	Fulfillment    syncFulfillment        `json:"fulfillment"`
}

type syncFulfillment struct {
	Status string   `json:"status"`
	Cards  []string `json:"cards"`
}

// New creates a TeamGenie sync service.
func New(cfg config.TeamGenieConfig, users usercontract.Store, queueClient *queue.Client) *Service {
	timeout := cfg.TimeoutMS
	if timeout <= 0 {
		timeout = 3000
	}
	return &Service{
		cfg:   cfg,
		users: users,
		queue: queueClient,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Millisecond,
		},
	}
}

// Enabled reports whether TeamGenie sync has enough configuration to run.
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled &&
		strings.TrimSpace(s.cfg.WebhookURL) != "" &&
		strings.TrimSpace(s.cfg.SharedSecret) != ""
}

// NotifyFulfilled enqueues a fulfilled-order sync and falls back to direct async delivery.
func (s *Service) NotifyFulfilled(order *orderdomain.Order) {
	if !s.Enabled() || order == nil || order.Fulfillment == nil {
		return
	}
	if s.queue != nil && s.queue.Enabled() {
		if err := s.queue.EnqueueTeamGenieSyncFulfilled(queue.TeamGenieSyncFulfilledPayload{OrderID: order.ID}); err != nil {
			logger.Warnw("fulfillment_enqueue_teamgenie_sync_failed",
				"order_id", order.ID,
				"order_no", order.OrderNo,
				"error", err,
			)
			logger.Infow("fulfillment_teamgenie_sync_fallback_direct_notify",
				"order_id", order.ID,
				"order_no", order.OrderNo,
				"reason", "enqueue_failed",
			)
			s.NotifyFulfilledDirect(order)
			return
		}
		logger.Infow("fulfillment_enqueue_teamgenie_sync_succeeded",
			"order_id", order.ID,
			"order_no", order.OrderNo,
			"fulfillment_id", order.Fulfillment.ID,
			"fulfillment_status", order.Fulfillment.Status,
		)
		return
	}
	logger.Infow("fulfillment_teamgenie_sync_fallback_direct_notify",
		"order_id", order.ID,
		"order_no", order.OrderNo,
		"reason", "queue_unavailable",
	)
	s.NotifyFulfilledDirect(order)
}

// NotifyFulfilledDirect runs sync in a goroutine. It is used only as a queue fallback.
func (s *Service) NotifyFulfilledDirect(order *orderdomain.Order) {
	if !s.Enabled() || order == nil || order.Fulfillment == nil {
		return
	}
	go func() {
		if err := s.SyncFulfilled(order); err != nil {
			logger.Warnw("teamgenie_sync_notify_failed",
				"order_id", order.ID,
				"order_no", order.OrderNo,
				"webhook_url", sanitizeWebhookURLForLog(s.cfg.WebhookURL),
				"error", err,
			)
			return
		}
		logger.Infow("teamgenie_sync_notify_succeeded",
			"order_id", order.ID,
			"order_no", order.OrderNo,
			"cards", len(splitPayloadCards(order.Fulfillment.Payload)),
		)
	}()
}

// SyncFulfilled runs a synchronous TeamGenie sync for worker retries.
func (s *Service) SyncFulfilled(order *orderdomain.Order) error {
	if !s.Enabled() || order == nil || order.Fulfillment == nil {
		return nil
	}
	payload, ok := s.buildPayload(order)
	if !ok {
		return nil
	}
	return s.post(payload)
}

func (s *Service) buildPayload(order *orderdomain.Order) (*syncPayload, bool) {
	cards := splitPayloadCards(order.Fulfillment.Payload)
	if len(cards) == 0 {
		return nil, false
	}

	payload := &syncPayload{
		OrderNo:        order.OrderNo,
		BuyerEmail:     s.resolveBuyerEmail(order),
		Price:          order.TotalAmount.String(),
		Currency:       strings.TrimSpace(order.Currency),
		Channel:        strings.TrimSpace(s.cfg.Channel),
		SourceOrderNo:  order.OrderNo,
		SourcePlatform: strings.TrimSpace(s.cfg.SourcePlatform),
		SourceSite:     strings.TrimSpace(s.cfg.SourceSite),
		SoldMetadata: map[string]interface{}{
			"fulfillment_status": strings.TrimSpace(order.Fulfillment.Status),
		},
		Fulfillment: syncFulfillment{
			Status: strings.TrimSpace(order.Fulfillment.Status),
			Cards:  cards,
		},
	}
	return payload, true
}

func (s *Service) resolveBuyerEmail(order *orderdomain.Order) string {
	if order == nil {
		return ""
	}
	if email := strings.TrimSpace(order.GuestEmail); email != "" {
		return email
	}
	if order.UserID == 0 || s.users == nil {
		return ""
	}
	user, err := s.users.GetByID(order.UserID)
	if err != nil {
		logger.Warnw("teamgenie_sync_resolve_buyer_email_failed",
			"order_id", order.ID,
			"order_no", order.OrderNo,
			"user_id", order.UserID,
			"error", err,
		)
		return ""
	}
	if user == nil {
		return ""
	}
	return strings.TrimSpace(user.Email)
}

func (s *Service) post(payload *syncPayload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, strings.TrimSpace(s.cfg.WebhookURL), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Dujiao-Sync-Secret", strings.TrimSpace(s.cfg.SharedSecret))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("sync webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return nil
}

func sanitizeWebhookURLForLog(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "<invalid>"
	}
	if u.User != nil {
		u.User = url.User("***")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

func splitPayloadCards(raw string) []string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	cards := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cards = append(cards, line)
	}
	return cards
}
