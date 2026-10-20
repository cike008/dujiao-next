package application

import (
	"context"
	"html"
	"regexp"
	"strings"

	"github.com/dujiao-next/internal/config"
	notifycontract "github.com/dujiao-next/internal/modules/telegram/notify/contract"
)

// Service 解析通知配置并编排 Telegram 消息发送。
type Service struct {
	settings   notifycontract.SettingReader
	defaultCfg config.TelegramAuthConfig
	sender     notifycontract.Sender
}

// NewService 创建 Telegram 通知应用服务。
func NewService(settings notifycontract.SettingReader, defaultCfg config.TelegramAuthConfig, sender notifycontract.Sender) *Service {
	if sender == nil {
		panic("telegram notify service: sender is nil")
	}
	return &Service{settings: settings, defaultCfg: defaultCfg, sender: sender}
}

// SendMessage 使用当前配置的 Bot Token 发送消息。
func (s *Service) SendMessage(ctx context.Context, chatID, message string) error {
	return s.sendFormatted(ctx, chatID, formatNotificationHTML(message), message)
}

// SendQuotedMessage 发送「标题 + 引用块正文」：标题在块外，正文放进 Telegram 引用块。
func (s *Service) SendQuotedMessage(ctx context.Context, chatID, title, body string) error {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if body == "" {
		return s.SendMessage(ctx, chatID, title)
	}
	htmlText := "<blockquote>" + formatNotificationHTML(body) + "</blockquote>"
	plain := body
	if title != "" {
		htmlText = formatNotificationHTML(title) + "\n\n" + htmlText
		plain = title + "\n\n" + body
	}
	return s.sendFormatted(ctx, chatID, htmlText, plain)
}

// sendFormatted 以 HTML 发送；若 Telegram 拒绝格式则退回纯文本，保证通知一定能送达。
func (s *Service) sendFormatted(ctx context.Context, chatID, htmlText, plainText string) error {
	token, err := s.resolveBotToken()
	if err != nil {
		return err
	}
	if token == "" {
		return notifycontract.ErrNotifyConfigInvalid
	}
	// 通知是纯文本：先整体转义，再把邮箱包成等宽格式。Telegram 会把裸邮箱识别成「写邮件」链接，
	// 并且会把紧挨着的中文标签一起划进链接；等宽格式既不会被当成链接，点一下还能直接复制。
	err = s.sender.SendWithBotToken(ctx, token, notifycontract.SendOptions{
		ChatID:                chatID,
		Message:               htmlText,
		ParseMode:             "HTML",
		DisableWebPagePreview: true,
	})
	if err != nil && strings.Contains(err.Error(), "parse entities") {
		return s.sender.SendWithBotToken(ctx, token, notifycontract.SendOptions{
			ChatID:                chatID,
			Message:               plainText,
			DisableWebPagePreview: true,
		})
	}
	return err
}

// SendWithBotToken 使用显式 Bot Token 发送消息。
func (s *Service) SendWithBotToken(ctx context.Context, botToken string, options notifycontract.SendOptions) error {
	return s.sender.SendWithBotToken(ctx, botToken, options)
}

func (s *Service) resolveBotToken() (string, error) {
	if s.settings == nil {
		return strings.TrimSpace(s.defaultCfg.BotToken), nil
	}
	setting, err := s.settings.GetTelegramAuthSetting(s.defaultCfg)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(setting.BotToken), nil
}

var notificationEmailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// formatNotificationHTML 把纯文本通知转成 Telegram HTML：全部转义，仅邮箱包成 <code>。
func formatNotificationHTML(message string) string {
	return notificationEmailPattern.ReplaceAllString(html.EscapeString(message), "<code>$0</code>")
}
