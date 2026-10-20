package application

import (
	"context"
	"errors"
	"testing"

	"github.com/dujiao-next/internal/config"
	settingssecurity "github.com/dujiao-next/internal/modules/settings/schema/security"
	notifycontract "github.com/dujiao-next/internal/modules/telegram/notify/contract"
)

type settingReaderStub struct {
	setting settingssecurity.TelegramAuthSetting
	err     error
	seen    config.TelegramAuthConfig
}

func (s *settingReaderStub) GetTelegramAuthSetting(defaultCfg config.TelegramAuthConfig) (settingssecurity.TelegramAuthSetting, error) {
	s.seen = defaultCfg
	return s.setting, s.err
}

type senderStub struct {
	token   string
	options notifycontract.SendOptions
	err     error
	calls   int
}

func (s *senderStub) SendWithBotToken(_ context.Context, token string, options notifycontract.SendOptions) error {
	s.calls++
	s.token = token
	s.options = options
	return s.err
}

func TestServiceSendMessageUsesDynamicBotToken(t *testing.T) {
	defaults := config.TelegramAuthConfig{BotToken: "default-token"}
	settings := &settingReaderStub{
		setting: settingssecurity.TelegramAuthSetting{BotToken: "  dynamic-token  "},
	}
	sender := &senderStub{}
	service := NewService(settings, defaults, sender)

	if err := service.SendMessage(context.Background(), "10001", "hello"); err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if settings.seen != defaults {
		t.Fatalf("unexpected default config: %#v", settings.seen)
	}
	if sender.calls != 1 || sender.token != "dynamic-token" {
		t.Fatalf("unexpected sender call: calls=%d token=%q", sender.calls, sender.token)
	}
	if sender.options.ChatID != "10001" || sender.options.Message != "hello" {
		t.Fatalf("unexpected send options: %#v", sender.options)
	}
	if !sender.options.DisableWebPagePreview {
		t.Fatal("configured notification must disable web page previews")
	}
}

func TestServiceSendMessageFallsBackToDefaultWithoutSettings(t *testing.T) {
	sender := &senderStub{}
	service := NewService(nil, config.TelegramAuthConfig{BotToken: "  default-token  "}, sender)

	if err := service.SendMessage(context.Background(), "10001", "hello"); err != nil {
		t.Fatalf("send message failed: %v", err)
	}
	if sender.calls != 1 || sender.token != "default-token" {
		t.Fatalf("unexpected sender call: calls=%d token=%q", sender.calls, sender.token)
	}
}

func TestServiceSendMessageRejectsMissingBotToken(t *testing.T) {
	sender := &senderStub{}
	service := NewService(&settingReaderStub{}, config.TelegramAuthConfig{}, sender)

	err := service.SendMessage(context.Background(), "10001", "hello")
	if !errors.Is(err, notifycontract.ErrNotifyConfigInvalid) {
		t.Fatalf("expected config invalid error, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender must not be called without a bot token, got %d calls", sender.calls)
	}
}

func TestServiceSendMessagePropagatesSettingReadFailure(t *testing.T) {
	wantErr := errors.New("settings unavailable")
	sender := &senderStub{}
	service := NewService(&settingReaderStub{err: wantErr}, config.TelegramAuthConfig{BotToken: "default-token"}, sender)

	err := service.SendMessage(context.Background(), "10001", "hello")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected setting error, got %v", err)
	}
	if sender.calls != 0 {
		t.Fatalf("sender must not be called after a setting error, got %d calls", sender.calls)
	}
}

func TestFormatNotificationHTMLEscapesEverythingAndWrapsOnlyEmails(t *testing.T) {
	cases := map[string]string{
		"购买人：zhangsan@example.com":      "购买人：<code>zhangsan@example.com</code>",
		"张三 <zhangsan@example.com>":     "张三 &lt;<code>zhangsan@example.com</code>&gt;",
		"a@b.co 和 c.d+e@f-g.org":        "<code>a@b.co</code> 和 <code>c.d+e@f-g.org</code>",
		"1 < 2 && <b>x</b> \"q\"":       "1 &lt; 2 &amp;&amp; &lt;b&gt;x&lt;/b&gt; &#34;q&#34;",
		"订单号：DJ2026 金额：220.00 CNY（无邮箱）": "订单号：DJ2026 金额：220.00 CNY（无邮箱）",
	}
	for in, want := range cases {
		if got := formatNotificationHTML(in); got != want {
			t.Errorf("formatNotificationHTML(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}

func TestServiceSendMessageUsesHTMLAndFallsBackToPlainTextWhenRejected(t *testing.T) {
	settings := &settingReaderStub{setting: settingssecurity.TelegramAuthSetting{BotToken: "tok"}}
	sender := &senderStub{}
	svc := NewService(settings, config.TelegramAuthConfig{}, sender)
	if err := svc.SendMessage(context.Background(), "1", "邮箱：a@b.co"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if sender.options.ParseMode != "HTML" || sender.options.Message != "邮箱：<code>a@b.co</code>" || sender.calls != 1 {
		t.Fatalf("unexpected first send: %+v calls=%d", sender.options, sender.calls)
	}

	rejecting := &rejectingSender{}
	svc = NewService(settings, config.TelegramAuthConfig{}, rejecting)
	if err := svc.SendMessage(context.Background(), "1", "邮箱：a@b.co"); err != nil {
		t.Fatalf("fallback send failed: %v", err)
	}
	if len(rejecting.sent) != 2 || rejecting.sent[1].ParseMode != "" || rejecting.sent[1].Message != "邮箱：a@b.co" {
		t.Fatalf("expected a plain-text retry after a parse error, got %+v", rejecting.sent)
	}

	other := &senderStub{err: errors.New("network down")}
	svc = NewService(settings, config.TelegramAuthConfig{}, other)
	if err := svc.SendMessage(context.Background(), "1", "x"); err == nil || other.calls != 1 {
		t.Fatalf("other errors must not trigger a retry (calls=%d err=%v)", other.calls, err)
	}
}

type rejectingSender struct{ sent []notifycontract.SendOptions }

func (r *rejectingSender) SendWithBotToken(_ context.Context, _ string, options notifycontract.SendOptions) error {
	r.sent = append(r.sent, options)
	if options.ParseMode == "HTML" {
		return errors.New("telegram status=400 body=Bad Request: can't parse entities")
	}
	return nil
}

func TestSendQuotedMessagePutsOnlyTheBodyInsideAQuoteBlock(t *testing.T) {
	settings := &settingReaderStub{setting: settingssecurity.TelegramAuthSetting{BotToken: "tok"}}
	sender := &senderStub{}
	svc := NewService(settings, config.TelegramAuthConfig{}, sender)
	if err := svc.SendQuotedMessage(context.Background(), "1", "✅ 订单支付成功", "购买人：a@b.co\n金额：1 <2>"); err != nil {
		t.Fatalf("send failed: %v", err)
	}
	want := "✅ 订单支付成功\n\n<blockquote>购买人：<code>a@b.co</code>\n金额：1 &lt;2&gt;</blockquote>"
	if sender.options.ParseMode != "HTML" || sender.options.Message != want {
		t.Fatalf("unexpected quoted message:\n got  %q\n want %q", sender.options.Message, want)
	}

	rejecting := &rejectingSender{}
	svc = NewService(settings, config.TelegramAuthConfig{}, rejecting)
	if err := svc.SendQuotedMessage(context.Background(), "1", "标题", "正文 a@b.co"); err != nil {
		t.Fatalf("fallback failed: %v", err)
	}
	if len(rejecting.sent) != 2 || rejecting.sent[1].ParseMode != "" || rejecting.sent[1].Message != "标题\n\n正文 a@b.co" {
		t.Fatalf("expected a plain-text retry without any markup, got %+v", rejecting.sent)
	}

	empty := &senderStub{}
	svc = NewService(settings, config.TelegramAuthConfig{}, empty)
	if err := svc.SendQuotedMessage(context.Background(), "1", "只有标题", "  "); err != nil || empty.options.Message != "只有标题" {
		t.Fatalf("an empty body must send just the title, got %q err=%v", empty.options.Message, err)
	}
}
