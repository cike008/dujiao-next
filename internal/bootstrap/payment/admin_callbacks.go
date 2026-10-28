package paymentbootstrap

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentcontract "github.com/dujiao-next/internal/modules/payment/contract"

	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"

	ordercontract "github.com/dujiao-next/internal/modules/order/contract"
	orderdomain "github.com/dujiao-next/internal/modules/order/domain"

	"github.com/dujiao-next/internal/constants"
	notificationcontract "github.com/dujiao-next/internal/modules/notification/contract"
	paymenttransport "github.com/dujiao-next/internal/modules/payment/transport/http"
	paymentcallbacktransport "github.com/dujiao-next/internal/modules/payment/transport/http/callback"
	walletcontract "github.com/dujiao-next/internal/modules/wallet/contract"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

type adminQueryAdapter struct {
	payments *paymentapp.PaymentService
}

func (a adminQueryAdapter) ListPayments(filter paymenttransport.AdminPaymentListFilter) ([]paymentdomain.Payment, int64, error) {
	return a.payments.ListPayments(paymentcontract.ListFilter{
		Page:         filter.Page,
		PageSize:     filter.PageSize,
		UserID:       filter.UserID,
		OrderID:      filter.OrderID,
		ChannelID:    filter.ChannelID,
		ProviderType: filter.ProviderType,
		ChannelType:  filter.ChannelType,
		Status:       filter.Status,
		CreatedFrom:  filter.CreatedFrom,
		CreatedTo:    filter.CreatedTo,
		SkipCount:    filter.SkipCount,
		Lightweight:  filter.Lightweight,
	})
}

func (a adminQueryAdapter) GetPayment(id uint) (*paymentdomain.Payment, error) {
	payment, err := a.payments.GetPayment(id)
	return payment, mapTransportError(err)
}

type adminPaymentConfirmerAdapter struct {
	payments *paymentapp.PaymentService
}

func (a adminPaymentConfirmerAdapter) ManualConfirmPayment(input paymenttransport.ManualConfirmPaymentInput) (*paymentdomain.Payment, error) {
	payment, err := a.payments.ManualConfirmPayment(paymentapp.ManualConfirmPaymentInput{
		OrderID:     input.OrderID,
		PaymentID:   input.PaymentID,
		AdminID:     input.AdminID,
		ProviderRef: input.ProviderRef,
		Remark:      input.Remark,
	})
	return payment, mapTransportError(err)
}

type adminChannelLookupAdapter struct {
	channels paymentcontract.ChannelStore
}

func (a adminChannelLookupAdapter) ListByIDs(ids []uint) ([]paymentdomain.PaymentChannel, error) {
	return a.channels.ListByIDs(ids)
}

type adminOrderLookupAdapter struct {
	orders ordercontract.Store
}

func (a adminOrderLookupAdapter) GetByIDs(ids []uint) ([]orderdomain.Order, error) {
	return a.orders.GetByIDs(ids)
}

type adminRechargeLookupAdapter struct {
	wallets walletcontract.Repository
}

func (a adminRechargeLookupAdapter) GetRechargeOrdersByPaymentIDs(paymentIDs []uint) ([]walletdomain.RechargeOrder, error) {
	if a.wallets == nil {
		return nil, nil
	}
	return a.wallets.GetRechargeOrdersByPaymentIDs(paymentIDs)
}

type adminChannelCatalogAdapter struct {
	payments *paymentapp.PaymentService
	channels paymentcontract.ChannelStore
}

func (a adminChannelCatalogAdapter) ValidateChannel(channel *paymentdomain.PaymentChannel) error {
	return mapTransportError(a.payments.ValidateChannel(channel))
}

func (a adminChannelCatalogAdapter) GetChannel(id uint) (*paymentdomain.PaymentChannel, error) {
	channel, err := a.payments.GetChannel(id)
	return channel, mapTransportError(err)
}

func (a adminChannelCatalogAdapter) ListChannels(filter paymenttransport.AdminChannelListFilter) ([]paymentdomain.PaymentChannel, int64, error) {
	return a.payments.ListChannels(paymentcontract.ChannelListFilter{
		Page:         filter.Page,
		PageSize:     filter.PageSize,
		ProviderType: filter.ProviderType,
		ChannelType:  filter.ChannelType,
		ActiveOnly:   filter.ActiveOnly,
	})
}

func (a adminChannelCatalogAdapter) Create(channel *paymentdomain.PaymentChannel) error {
	return a.channels.Create(channel)
}

func (a adminChannelCatalogAdapter) Update(channel *paymentdomain.PaymentChannel) error {
	return a.channels.Update(channel)
}

func (a adminChannelCatalogAdapter) Delete(id uint) error {
	return a.channels.Delete(id)
}

func (a adminChannelCatalogAdapter) TestChannelSecurity(ctx context.Context, id uint) (*paymentcontract.GatewaySecurityTestResult, error) {
	result, err := a.payments.TestChannelSecurity(ctx, id)
	return result, mapTransportError(err)
}

type webhookServiceAdapter struct {
	payments *paymentapp.PaymentService
}

type callbackServiceAdapter struct {
	payments *paymentapp.PaymentService
}

func (a callbackServiceAdapter) HandleSyncCallback(channel *paymentdomain.PaymentChannel, form map[string][]string, body []byte) (*paymentdomain.Payment, error) {
	return a.payments.HandleSyncCallback(channel, form, body)
}

func (a callbackServiceAdapter) HandleWechatWebhook(input paymentcallbacktransport.WechatWebhookInput) (*paymentdomain.Payment, string, error) {
	payment, eventType, err := a.payments.HandleWechatWebhook(paymentapp.WebhookCallbackInput{
		ChannelID: input.ChannelID,
		Headers:   input.Headers,
		Body:      input.Body,
		Context:   input.Context,
	})
	return payment, eventType, mapTransportError(err)
}

func (a webhookServiceAdapter) HandlePaypalWebhook(input paymenttransport.WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	payment, eventType, err := a.payments.HandlePaypalWebhook(paymentapp.WebhookCallbackInput{
		ChannelID: input.ChannelID,
		Headers:   input.Headers,
		Body:      input.Body,
		Context:   input.Context,
	})
	return payment, eventType, mapTransportError(err)
}

func (a webhookServiceAdapter) HandleStripeWebhook(input paymenttransport.WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	payment, eventType, err := a.payments.HandleStripeWebhook(paymentapp.WebhookCallbackInput{
		ChannelID: input.ChannelID,
		Headers:   input.Headers,
		Body:      input.Body,
		Context:   input.Context,
	})
	return payment, eventType, mapTransportError(err)
}

func (a webhookServiceAdapter) HandleDujiaoPayWebhook(input paymenttransport.WebhookCallbackInput) (*paymentdomain.Payment, string, error) {
	payment, eventType, err := a.payments.HandleDujiaoPayWebhook(paymentapp.WebhookCallbackInput{
		ChannelID: input.ChannelID,
		Headers:   input.Headers,
		Body:      input.Body,
		Context:   input.Context,
	})
	return payment, eventType, mapTransportError(err)
}

type exceptionAlerterAdapter struct {
	notifications notificationcontract.NotificationEnqueuer
	throttle      *exceptionAlertThrottle
}

func (a exceptionAlerterAdapter) EnqueuePaymentExceptionAlert(method, path, clientIP string, data jsonmap.JSON) error {
	if a.notifications == nil {
		return nil
	}
	send, suppressed := a.throttle.allow(data)
	if !send {
		return nil
	}
	payload := jsonmap.JSON{
		"source":      constants.NotificationBizTypePaymentCallback,
		"method":      method,
		"path":        path,
		"client_ip":   clientIP,
		"occurred_at": time.Now().Format("2006-01-02 15:04:05"),
	}
	for key, value := range data {
		payload[key] = value
	}
	if suppressed > 0 {
		payload["suppressed_count"] = suppressed
		payload["suppressed_window"] = exceptionAlertWindow.String()
	}
	return a.notifications.Enqueue(notificationcontract.EnqueueInput{
		EventType: constants.NotificationEventExceptionAlert,
		BizType:   constants.NotificationBizTypePaymentCallback,
		BizID:     0,
		Data:      payload,
	})
}

const (
	// exceptionAlertWindow 同一类告警在该窗口内只发送一次，其余计入 suppressed_count。
	exceptionAlertWindow = 10 * time.Minute
	// exceptionAlertMaxKeys 限制内存占用；超过后先清理已过期条目。
	exceptionAlertMaxKeys = 1024
)

// exceptionAlertThrottle 聚合重复的支付异常告警，避免扫描器刷屏淹没真实告警。
//
// 聚合键 = 告警类型 + 渠道 + 具体的支付/订单标识。没有具体标识的告警（例如伪造回调的盲扫）
// 会合并成一条；针对某一笔支付的真实失败各自独立，不会被别的告警吞掉。
type exceptionAlertThrottle struct {
	mu      sync.Mutex
	window  time.Duration
	entries map[string]*throttledAlert
	now     func() time.Time
}

type throttledAlert struct {
	sentAt     time.Time
	suppressed int
}

func newExceptionAlertThrottle(window time.Duration) *exceptionAlertThrottle {
	return &exceptionAlertThrottle{window: window, entries: map[string]*throttledAlert{}, now: time.Now}
}

func throttleField(data jsonmap.JSON, key string) string {
	if value, ok := data[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func exceptionAlertKey(data jsonmap.JSON) string {
	return strings.Join([]string{
		throttleField(data, "alert_type"),
		throttleField(data, "provider"),
		throttleField(data, "payment_id"),
		throttleField(data, "out_trade_no"),
		throttleField(data, "order_no"),
	}, "|")
}

// allow 返回是否应该发送这条告警；发送时返回此前被压制的数量（可写入告警内容）。
func (t *exceptionAlertThrottle) allow(data jsonmap.JSON) (send bool, suppressed int) {
	if t == nil || t.window <= 0 {
		return true, 0
	}
	key := exceptionAlertKey(data)
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()
	entry, found := t.entries[key]
	if found && now.Sub(entry.sentAt) < t.window {
		entry.suppressed++
		return false, 0
	}
	if len(t.entries) >= exceptionAlertMaxKeys {
		for k, e := range t.entries {
			if now.Sub(e.sentAt) >= t.window {
				delete(t.entries, k)
			}
		}
		if len(t.entries) >= exceptionAlertMaxKeys {
			// 全部都还在窗口内：宁可多发也不丢告警。
			return true, 0
		}
	}
	previous := 0
	if found {
		previous = entry.suppressed
	}
	t.entries[key] = &throttledAlert{sentAt: now}
	return true, previous
}
