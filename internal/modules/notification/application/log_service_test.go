package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/modules/notification/application/format"
	"github.com/dujiao-next/internal/modules/notification/contract"
	"github.com/dujiao-next/internal/modules/notification/domain"
	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	settingsstorefront "github.com/dujiao-next/internal/modules/settings/schema/storefront"
	"github.com/dujiao-next/internal/queue"
)

type notificationSettingsStub struct {
	notification settingsmessaging.NotificationCenterSetting
	dashboard    settingsstorefront.DashboardSetting
}

func (s notificationSettingsStub) GetNotificationCenterSetting() (settingsmessaging.NotificationCenterSetting, error) {
	return s.notification, nil
}

func (s notificationSettingsStub) GetDashboardSetting() (settingsstorefront.DashboardSetting, error) {
	return s.dashboard, nil
}

type notificationEmailStub struct{}

func (notificationEmailStub) SendCustomEmail(recipient, _, _ string) error {
	if strings.Contains(recipient, "failure") {
		return errors.New("simulated email failure")
	}
	return nil
}

type notificationFeishuCall struct {
	appID         string
	appSecret     string
	receiveIDType string
	receiveID     string
	message       string
}

type notificationFeishuStub struct {
	calls []notificationFeishuCall
}

func (s *notificationFeishuStub) SendMessage(_ context.Context, appID, appSecret, receiveIDType, receiveID, message string) error {
	s.calls = append(s.calls, notificationFeishuCall{
		appID:         appID,
		appSecret:     appSecret,
		receiveIDType: receiveIDType,
		receiveID:     receiveID,
		message:       message,
	})
	if strings.Contains(receiveID, "failure") {
		return errors.New("simulated feishu failure")
	}
	return nil
}

type notificationLogRepositoryStub struct {
	items []domain.NotificationLog
}

func (r *notificationLogRepositoryStub) Create(item *domain.NotificationLog) error {
	if item != nil {
		copy := *item
		copy.ID = uint(len(r.items) + 1)
		r.items = append(r.items, copy)
	}
	return nil
}

func (r *notificationLogRepositoryStub) ListAdmin(filter contract.LogListFilter) ([]domain.NotificationLog, int64, error) {
	result := make([]domain.NotificationLog, 0, len(r.items))
	for _, item := range r.items {
		if filter.EventType != "" && item.EventType != filter.EventType {
			continue
		}
		if filter.IsTest != nil && item.IsTest != *filter.IsTest {
			continue
		}
		result = append(result, item)
	}
	return result, int64(len(result)), nil
}

func setupLogService(t *testing.T) (*Service, *LogService) {
	t.Helper()
	logService := NewLogService(&notificationLogRepositoryStub{})
	setting := settingsmessaging.NotificationCenterSetting{
		DefaultLocale: constants.LocaleEnUS,
		Scenes: settingsmessaging.NotificationSceneSetting{
			OrderPaidSuccess: true,
			ExceptionAlert:   true,
		},
		Channels: settingsmessaging.NotificationChannelsSetting{
			Email: settingsmessaging.NotificationChannelSetting{
				Enabled:    true,
				Recipients: []string{"success@example.com", "failure@example.com"},
			},
		},
		Templates: settingsmessaging.NotificationTemplatesSetting{
			OrderPaidSuccess: settingsmessaging.NotificationSceneTemplate{
				ENUS: settingsmessaging.NotificationLocalizedTemplate{
					Title: "Order {{order_no}}",
					Body:  "Customer {{customer_email}}",
				},
			},
			ExceptionAlert: settingsmessaging.NotificationSceneTemplate{
				ENUS: settingsmessaging.NotificationLocalizedTemplate{
					Title: "Alert {{message}}",
					Body:  "{{message}}",
				},
			},
		},
	}
	service := NewService(notificationSettingsStub{notification: setting}, notificationEmailStub{}, nil, nil, logService, nil, nil)
	return service, logService
}

func TestServiceSendTestRecordsSuccessLog(t *testing.T) {
	service, logService := setupLogService(t)
	if err := service.SendTest(context.Background(), contract.TestSendInput{
		Channel: "email",
		Target:  "success@example.com",
		Scene:   constants.NotificationEventOrderPaidSuccess,
		Locale:  constants.LocaleEnUS,
	}); err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}

	isTest := true
	items, total, err := logService.ListForAdmin(contract.LogListFilter{
		Page: 1, PageSize: 10, EventType: constants.NotificationEventOrderPaidSuccess, IsTest: &isTest,
	})
	if err != nil {
		t.Fatalf("list notification logs failed: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("expected 1 test log, total=%d len=%d", total, len(items))
	}
	if items[0].Status != notificationLogStatusSuccess || !items[0].IsTest {
		t.Fatalf("unexpected log: %#v", items[0])
	}
	if !strings.Contains(items[0].Title, "DJ202603230001") {
		t.Fatalf("title should include sample order no, got %s", items[0].Title)
	}
}

func TestServiceDispatchSingleEventRecordsPerRecipientResult(t *testing.T) {
	service, logService := setupLogService(t)
	setting, err := service.settingService.GetNotificationCenterSetting()
	if err != nil {
		t.Fatalf("get notification center setting failed: %v", err)
	}
	dispatchErr := service.dispatchSingleEvent(context.Background(), setting, queue.NotificationDispatchPayload{
		EventType: constants.NotificationEventOrderPaidSuccess,
		BizType:   constants.NotificationBizTypeOrder,
		BizID:     88,
		Locale:    constants.LocaleEnUS,
		Force:     true,
		Data: map[string]interface{}{
			"order_no":       "DJ-LOG-88",
			"customer_email": "member@example.com",
		},
	})
	if !errors.Is(dispatchErr, contract.ErrSendFailed) {
		t.Fatalf("expected send failure, got %v", dispatchErr)
	}

	items, total, err := logService.ListForAdmin(contract.LogListFilter{Page: 1, PageSize: 10, EventType: constants.NotificationEventOrderPaidSuccess})
	if err != nil {
		t.Fatalf("list notification logs failed: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected 2 recipient logs, total=%d len=%d", total, len(items))
	}
	statuses := map[string]string{}
	for _, item := range items {
		statuses[item.Recipient] = item.Status
	}
	if statuses["success@example.com"] != notificationLogStatusSuccess {
		t.Fatalf("success recipient status mismatch: %v", statuses)
	}
	if statuses["failure@example.com"] != notificationLogStatusFailed {
		t.Fatalf("failure recipient status mismatch: %v", statuses)
	}
}

func TestServiceDispatchSingleEventSendsFeishuAndRecordsEachRecipient(t *testing.T) {
	logService := NewLogService(&notificationLogRepositoryStub{})
	feishuSender := &notificationFeishuStub{}
	setting := settingsmessaging.NotificationCenterDefaultSetting()
	setting.DefaultLocale = constants.LocaleZhCN
	setting.Channels.Feishu = settingsmessaging.FeishuNotificationChannelSetting{
		Enabled:       true,
		AppID:         "cli_demo",
		AppSecret:     "secret",
		ReceiveIDType: settingsmessaging.FeishuReceiveIDTypeChatID,
		Recipients:    []string{"oc_success", "oc_failure"},
	}
	service := NewService(notificationSettingsStub{notification: setting}, nil, nil, nil, logService, nil, feishuSender)

	err := service.dispatchSingleEvent(context.Background(), setting, queue.NotificationDispatchPayload{
		EventType: constants.NotificationEventOrderPaidSuccess,
		BizType:   constants.NotificationBizTypeOrder,
		BizID:     99,
		Locale:    constants.LocaleZhCN,
		Force:     true,
		Data: map[string]interface{}{
			"order_no":       "DJ-FEISHU-99",
			"customer_email": "member@example.com",
		},
	})
	if !errors.Is(err, contract.ErrSendFailed) {
		t.Fatalf("expected one feishu failure, got %v", err)
	}
	if len(feishuSender.calls) != 2 {
		t.Fatalf("expected two feishu calls, got %d", len(feishuSender.calls))
	}
	firstCall := feishuSender.calls[0]
	if firstCall.appID != "cli_demo" || firstCall.appSecret != "secret" || firstCall.receiveIDType != settingsmessaging.FeishuReceiveIDTypeChatID {
		t.Fatalf("unexpected feishu credentials or id type: %#v", firstCall)
	}
	if !strings.Contains(firstCall.message, "DJ-FEISHU-99") {
		t.Fatalf("rendered feishu message missing order number: %q", firstCall.message)
	}

	items, total, listErr := logService.ListForAdmin(contract.LogListFilter{
		Page: 1, PageSize: 10, Channel: constants.NotificationChannelFeishu,
	})
	if listErr != nil {
		t.Fatalf("list feishu notification logs: %v", listErr)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("expected two feishu logs, total=%d len=%d", total, len(items))
	}
	statuses := map[string]string{}
	for _, item := range items {
		statuses[item.Recipient] = item.Status
	}
	if statuses["oc_success"] != notificationLogStatusSuccess || statuses["oc_failure"] != notificationLogStatusFailed {
		t.Fatalf("unexpected feishu log statuses: %#v", statuses)
	}
}

func derivedVariables(eventType, locale string, data map[string]interface{}) map[string]interface{} {
	vars := format.BuildTemplateVariables(queue.NotificationDispatchPayload{EventType: eventType, Data: data})
	format.AddDerivedVariables(vars, locale)
	return vars
}

func TestTemplateVariablesIncludeAlertIconAndReadableChannelName(t *testing.T) {
	icons := map[string][2]string{
		"error": {"❌", "🟥🟥🟥"}, "ERROR": {"❌", "🟥🟥🟥"}, "critical": {"❌", "🟥🟥🟥"},
		"warning": {"⚠️", "🟧🟧🟧"}, "warn": {"⚠️", "🟧🟧🟧"}, "info": {"ℹ️", "🟪🟪🟪"}, "weird": {"", ""},
	}
	for level, want := range icons {
		vars := derivedVariables("exception_alert", "zh-CN", map[string]interface{}{"alert_level": level})
		if vars["alert_level_icon"] != want[0] || vars["alert_level_bar"] != want[1] {
			t.Errorf("alert_level=%q: icon %q bar %q, want %q", level, vars["alert_level_icon"], vars["alert_level_bar"], want)
		}
	}
	channels := []struct{ provider, channel, want string }{
		{"official", "stripe", "Stripe"}, {"official", "paypal", "PayPal"}, {"official", "alipay", "支付宝"},
		{"official", "wechat", "微信支付"}, {"epay", "alipay", "易支付 · 支付宝"},
		{"bepusdt", "usdt.arbitrum", "BEPUSDT · usdt.arbitrum"}, {"bepusdt", "bepusdt", "BEPUSDT"},
		{"dujiaopay", "tron-usdt", "DujiaoPay · tron-usdt"}, {"wallet", "balance", "余额"}, {"official", "", ""},
	}
	for _, c := range channels {
		vars := derivedVariables("order_paid_success", "zh-CN", map[string]interface{}{"provider_type": c.provider, "channel_type": c.channel})
		if vars["payment_channel_name"] != c.want {
			t.Errorf("%s/%s: name %q, want %q", c.provider, c.channel, vars["payment_channel_name"], c.want)
		}
	}
	plain := derivedVariables("order_paid_success", "zh-CN", map[string]interface{}{"order_no": "A1"})
	if _, ok := plain["alert_level_icon"]; ok {
		t.Error("alert_level_icon must only be added when the payload carries alert_level")
	}
	rendered := format.RenderTemplate("✅ {{payment_channel_name}} · {{customer_email}}",
		map[string]interface{}{"payment_channel_name": "Stripe", "customer_email": "a@b.com"})
	if rendered != "✅ Stripe · a@b.com" {
		t.Errorf("render mismatch: %q", rendered)
	}
	if missing := format.RenderTemplate("[{{alert_level_icon}}]", map[string]interface{}{}); missing != "[]" {
		t.Errorf("a missing variable must render empty, got %q", missing)
	}
}

func TestOptionalLinesDisappearCleanlyWhenTheyHaveNoValue(t *testing.T) {
	const tmpl = "{{alert_level_icon}} 级别：{{alert_level}}\n🕒 时间：{{occurred_at}}{{client_ip_suffix}}\n{{alert_metrics}}\n{{affected_items_summary}}"
	render := func(data map[string]interface{}) string {
		vars := derivedVariables("exception_alert", "zh-CN", data)
		vars["occurred_at"] = "12:00:00"
		return format.ComposePlainTextMessage("🚨 告警", format.RenderTemplate(tmpl, vars))
	}
	// 支付回调类告警：有来源 IP，没有数值与受影响商品 → 末尾的空行被裁掉，IP 接在时间后面。
	callback := render(map[string]interface{}{"alert_level": "error", "client_ip": "1.2.3.4"})
	if callback != "🚨 告警\n\n❌ 级别：error\n🕒 时间：12:00:00｜🌐 1.2.3.4" {
		t.Errorf("callback alert rendered unexpectedly: %q", callback)
	}
	// 库存类告警：没有来源 IP，有数值与商品。
	stock := render(map[string]interface{}{"alert_level": "warning", "alert_value": "2", "alert_threshold": "5", "affected_items_summary": "1. A"})
	if stock != "🚨 告警\n\n⚠️ 级别：warning\n🕒 时间：12:00:00\n📊 当前值：2　阈值：5\n1. A" {
		t.Errorf("stock alert rendered unexpectedly: %q", stock)
	}
	if en := derivedVariables("exception_alert", "en-US", map[string]interface{}{"alert_value": "2", "alert_threshold": "5"})["alert_metrics"]; en != "📊 Current: 2　Threshold: 5" {
		t.Errorf("english metrics line: %q", en)
	}
}

type telegramRoutingStub struct{ plain, quoted []string }

func (s *telegramRoutingStub) SendMessage(_ context.Context, _ string, message string) error {
	s.plain = append(s.plain, message)
	return nil
}

func (s *telegramRoutingStub) SendQuotedMessage(_ context.Context, _ string, title, body string) error {
	s.quoted = append(s.quoted, title+"|"+body)
	return nil
}

func TestOnlyOrderPaidSuccessUsesTheTelegramQuoteBlock(t *testing.T) {
	stub := &telegramRoutingStub{}
	svc := &Service{telegramSender: stub}
	for _, scene := range []string{
		constants.NotificationEventOrderPaidSuccess,
		constants.NotificationEventWalletRechargeSuccess,
		constants.NotificationEventManualFulfillmentPending,
		constants.NotificationEventExceptionAlert,
	} {
		if err := svc.sendTelegram(context.Background(), "1", scene, "T", "B"); err != nil {
			t.Fatalf("%s: %v", scene, err)
		}
	}
	if len(stub.quoted) != 1 || stub.quoted[0] != "T|B" {
		t.Fatalf("only the paid-order notification may use the quote block, got %v", stub.quoted)
	}
	if len(stub.plain) != 3 || stub.plain[0] != "T\n\nB" {
		t.Fatalf("every other scene must stay plain text, got %v", stub.plain)
	}
}
