package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dujiao-next/internal/config"
	"github.com/dujiao-next/internal/logger"
	"github.com/dujiao-next/internal/models"
)

type TeamGenieSyncService struct {
	cfg        config.TeamGenieSyncConfig
	httpClient *http.Client
}

type teamGenieSyncPayload struct {
	OrderNo        string                 `json:"order_no"`
	BuyerEmail     string                 `json:"buyer_email,omitempty"`
	Price          string                 `json:"price,omitempty"`
	Currency       string                 `json:"currency,omitempty"`
	Channel        string                 `json:"channel,omitempty"`
	SourceOrderNo  string                 `json:"source_order_no,omitempty"`
	SourcePlatform string                 `json:"source_platform,omitempty"`
	SourceSite     string                 `json:"source_site,omitempty"`
	SoldMetadata   map[string]interface{} `json:"sold_metadata,omitempty"`
	Fulfillment    teamGenieFulfillment   `json:"fulfillment"`
}

type teamGenieFulfillment struct {
	Status string   `json:"status"`
	Cards  []string `json:"cards"`
}

func NewTeamGenieSyncService(cfg config.TeamGenieSyncConfig) *TeamGenieSyncService {
	timeout := cfg.TimeoutMS
	if timeout <= 0 {
		timeout = 3000
	}
	return &TeamGenieSyncService{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(timeout) * time.Millisecond,
		},
	}
}

func (s *TeamGenieSyncService) Enabled() bool {
	return s != nil && s.cfg.Enabled &&
		strings.TrimSpace(s.cfg.WebhookURL) != "" &&
		strings.TrimSpace(s.cfg.SharedSecret) != ""
}

func (s *TeamGenieSyncService) NotifyFulfilled(order *models.Order, fulfillment *models.Fulfillment) {
	if !s.Enabled() || order == nil || fulfillment == nil {
		return
	}

	go func() {
		if err := s.SyncFulfilled(order, fulfillment); err != nil {
			logger.Warnw("teamgenie_sync_notify_failed",
				"order_id", order.ID,
				"order_no", order.OrderNo,
				"webhook_url", s.cfg.WebhookURL,
				"error", err,
			)
			return
		}
		logger.Infow("teamgenie_sync_notify_succeeded",
			"order_id", order.ID,
			"order_no", order.OrderNo,
			"cards", len(splitPayloadCards(fulfillment.Payload)),
		)
	}()
}

// SyncFulfilled 同步执行 TeamGenie 售出同步，供 worker 重试调用。
func (s *TeamGenieSyncService) SyncFulfilled(order *models.Order, fulfillment *models.Fulfillment) error {
	if !s.Enabled() || order == nil || fulfillment == nil {
		return nil
	}
	payload, ok := s.buildPayload(order, fulfillment)
	if !ok {
		return nil
	}
	return s.post(payload)
}

func (s *TeamGenieSyncService) buildPayload(order *models.Order, fulfillment *models.Fulfillment) (*teamGenieSyncPayload, bool) {
	cards := splitPayloadCards(fulfillment.Payload)
	if len(cards) == 0 {
		return nil, false
	}

	payload := &teamGenieSyncPayload{
		OrderNo:        order.OrderNo,
		BuyerEmail:     strings.TrimSpace(order.GuestEmail),
		Price:          order.TotalAmount.String(),
		Currency:       strings.TrimSpace(order.Currency),
		Channel:        strings.TrimSpace(s.cfg.Channel),
		SourceOrderNo:  order.OrderNo,
		SourcePlatform: strings.TrimSpace(s.cfg.SourcePlatform),
		SourceSite:     strings.TrimSpace(s.cfg.SourceSite),
		SoldMetadata: map[string]interface{}{
			"fulfillment_status": strings.TrimSpace(fulfillment.Status),
		},
		Fulfillment: teamGenieFulfillment{
			Status: strings.TrimSpace(fulfillment.Status),
			Cards:  cards,
		},
	}
	return payload, true
}

func (s *TeamGenieSyncService) post(payload *teamGenieSyncPayload) error {
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
