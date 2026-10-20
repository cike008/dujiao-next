package format

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	settingsmessaging "github.com/dujiao-next/internal/modules/settings/schema/messaging"
	"github.com/dujiao-next/internal/queue"
	"github.com/dujiao-next/internal/shared/jsonmap"
)

var notificationTemplateVariablePattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_]+)\s*\}\}`)

func BuildTemplateVariables(payload queue.NotificationDispatchPayload) map[string]interface{} {
	data := CloneVariables(payload.Data)
	if data == nil {
		data = map[string]interface{}{}
	}
	data["event_type"] = strings.ToLower(strings.TrimSpace(payload.EventType))
	data["biz_type"] = strings.TrimSpace(payload.BizType)
	data["biz_id"] = fmt.Sprintf("%d", payload.BizID)
	data["occurred_at"] = time.Now().Format("2006-01-02 15:04:05")
	return data
}

// AddDerivedVariables 补充供模板直接使用的派生变量（真实通知与测试通知共用）：
//   - alert_level_icon：按告警级别给出 ❌ / ⚠️ / ℹ️
//   - alert_level_bar：按告警级别给出彩色方块色条（错误 🟥🟥🟥 / 警告 🟧🟧🟧 / 提示 🟪🟪🟪），适合放在标题里
//   - payment_channel_name：可读的支付渠道名（Stripe、PayPal、易支付 · 支付宝 …）
//   - client_ip_suffix：有来源 IP 时为「｜🌐 IP」，否则为空，可直接接在时间等行末尾
//   - alert_metrics：有当前值时为一整行「📊 当前值…阈值…」，否则为空，适合放在模板靠后的位置
//
// 值首尾不含空白（渲染时会被裁剪），所以用「｜」而不是空格作分隔。
func AddDerivedVariables(data map[string]interface{}, locale string) {
	if data == nil {
		return
	}
	if level, ok := data["alert_level"]; ok {
		data["alert_level_icon"] = alertLevelIcon(fmt.Sprint(level))
		data["alert_level_bar"] = alertLevelBar(fmt.Sprint(level))
	}
	data["payment_channel_name"] = paymentChannelDisplayName(
		strings.TrimSpace(fmt.Sprint(valueOrEmpty(data["provider_type"]))),
		strings.TrimSpace(fmt.Sprint(valueOrEmpty(data["channel_type"]))),
	)
	if ip := strings.TrimSpace(fmt.Sprint(valueOrEmpty(data["client_ip"]))); ip != "" {
		data["client_ip_suffix"] = "｜🌐 " + ip
	} else {
		data["client_ip_suffix"] = ""
	}
	value := strings.TrimSpace(fmt.Sprint(valueOrEmpty(data["alert_value"])))
	threshold := strings.TrimSpace(fmt.Sprint(valueOrEmpty(data["alert_threshold"])))
	if value == "" {
		data["alert_metrics"] = ""
		return
	}
	data["alert_metrics"] = localizedNotificationText(locale,
		"📊 当前值："+value+"　阈值："+threshold,
		"📊 當前值："+value+"　閾值："+threshold,
		"📊 Current: "+value+"　Threshold: "+threshold)
}

func valueOrEmpty(value interface{}) interface{} {
	if value == nil {
		return ""
	}
	return value
}

// alertLevelIcon 把告警级别转成图标，供通知模板用 {{alert_level_icon}} 引用。
func alertLevelIcon(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical", "fatal", "error":
		return "❌"
	case "warning", "warn":
		return "⚠️"
	case "info":
		return "ℹ️"
	default:
		return ""
	}
}

// alertLevelBar 把告警级别转成三个同色方块，供标题用 {{alert_level_bar}} 引用。
func alertLevelBar(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "critical", "fatal", "error":
		return "🟥🟥🟥"
	case "warning", "warn":
		return "🟧🟧🟧"
	case "info":
		return "🟪🟪🟪" // 蓝色已用于「充值成功」，提示改用紫色避免撞色
	default:
		return ""
	}
}

var paymentChannelNames = map[string]string{
	"stripe": "Stripe", "paypal": "PayPal", "alipay": "支付宝", "wechat": "微信支付", "wxpay": "微信支付",
	"qqpay": "QQ 钱包", "usdt": "USDT", "trx": "TRX", "balance": "余额",
}

var paymentProviderNames = map[string]string{
	"epay": "易支付", "epusdt": "EPUSDT", "bepusdt": "BEPUSDT", "dujiaopay": "DujiaoPay",
	"okpay": "OKPay", "tokenpay": "TokenPay",
}

// paymentChannelDisplayName 生成适合直接阅读的支付渠道名，供模板用 {{payment_channel_name}} 引用。
// official/wallet 只是“接入方式”，不展示；第三方聚合渠道展示为「接入方 · 渠道」。
func paymentChannelDisplayName(providerType, channelType string) string {
	provider := strings.ToLower(providerType)
	channel := strings.ToLower(channelType)
	channelName := channelType
	if name, ok := paymentChannelNames[channel]; ok {
		channelName = name
	}
	if providerName, ok := paymentProviderNames[provider]; ok {
		if channelName == "" || strings.EqualFold(channelName, providerName) {
			return providerName
		}
		return providerName + " · " + channelName
	}
	return channelName
}

func RenderTemplate(tmpl string, variables map[string]interface{}) string {
	tmpl = strings.TrimSpace(tmpl)
	if tmpl == "" {
		return ""
	}
	return notificationTemplateVariablePattern.ReplaceAllStringFunc(tmpl, func(matched string) string {
		submatch := notificationTemplateVariablePattern.FindStringSubmatch(matched)
		if len(submatch) != 2 {
			return matched
		}
		value, ok := variables[strings.TrimSpace(submatch[1])]
		if !ok {
			return ""
		}
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	})
}

func ResolveLocale(locale, fallback string) string {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		locale = strings.TrimSpace(fallback)
	}
	return normalizeNotificationLocale(locale)
}

func ComposePlainTextMessage(title, body string) string {
	title = strings.TrimSpace(title)
	body = strings.TrimSpace(body)
	if title == "" {
		return body
	}
	if body == "" {
		return title
	}
	return title + "\n\n" + body
}

func JSONToMap(data jsonmap.JSON) map[string]interface{} {
	if data == nil {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{}, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}

func CloneVariables(data map[string]interface{}) map[string]interface{} {
	if len(data) == 0 {
		return map[string]interface{}{}
	}
	result := make(map[string]interface{}, len(data))
	for key, value := range data {
		result[key] = value
	}
	return result
}

func normalizeNotificationLocale(locale string) string {
	return settingsmessaging.NormalizeNotificationLocale(locale)
}

func normalizeNotificationInventoryAlertInterval(seconds int) int {
	return settingsmessaging.NormalizeNotificationInventoryAlertInterval(seconds)
}

func normalizeNotificationPaymentOrderAlertInterval(seconds int) int {
	return settingsmessaging.NormalizeNotificationPaymentOrderAlertInterval(seconds)
}
