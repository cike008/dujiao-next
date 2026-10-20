package middleware

import (
	"net/http"
	"strings"

	settingsapp "github.com/dujiao-next/internal/modules/settings/application"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"

	"github.com/dujiao-next/internal/constants"
	paymenttransport "github.com/dujiao-next/internal/modules/payment/transport/http"
	paymentcallbacktransport "github.com/dujiao-next/internal/modules/payment/transport/http/callback"
	upstreamtransport "github.com/dujiao-next/internal/modules/upstreamapi/transport/http"

	"github.com/gin-gonic/gin"
)

// defaultCallbackPaths 默认回调路径集合，用于在配置自定义路由后屏蔽默认路径
var defaultCallbackPaths = map[string]bool{
	constants.DefaultPaymentCallbackPath:  true,
	constants.DefaultDujiaoPayWebhookPath: true,
	constants.DefaultPaypalWebhookPath:    true,
	constants.DefaultStripeWebhookPath:    true,
	constants.DefaultUpstreamCallbackPath: true,
}

// callbackDispatchers 是自定义回调路径对应的处理函数；nil 表示该入口不可用。
type callbackDispatchers struct {
	payment   gin.HandlerFunc
	dujiaoPay gin.HandlerFunc
	paypal    gin.HandlerFunc
	stripe    gin.HandlerFunc
	upstream  gin.HandlerFunc
}

// CallbackRouteMiddleware 动态回调路由中间件。
// 当管理员配置了自定义回调路由时：
//   - 匹配自定义路径 → 先经过 rateLimit，再分发到对应 handler
//   - 匹配默认回调路径 → 返回 404（隐藏默认路径）
//   - 未配置自定义路由 → 放行，默认路由正常工作
//
// 自定义路径在路由层之前就被分发，所以不会经过默认回调路由组上的限流；
// rateLimit 必须由调用方传入与默认回调路由相同的限流，否则自定义路径没有任何限流保护。
func CallbackRouteMiddleware(
	settingService *settingsapp.Service,
	callbackHandler *paymentcallbacktransport.Handler,
	webhookHandler *paymenttransport.WebhookHandler,
	upstreamHandler *upstreamtransport.Handler,
	rateLimit gin.HandlerFunc,
) gin.HandlerFunc {
	dispatch := callbackDispatchers{
		payment:  callbackHandler.PaymentCallback,
		upstream: upstreamHandler.HandleCallback,
	}
	if webhookHandler != nil {
		dispatch.dujiaoPay = webhookHandler.DujiaoPayWebhook
		dispatch.paypal = webhookHandler.PaypalWebhook
		dispatch.stripe = webhookHandler.StripeWebhook
	}
	return newCallbackRouteMiddleware(settingService.GetCallbackRoutesCached, dispatch, rateLimit)
}

func newCallbackRouteMiddleware(
	getRoutes func() *settingsintegration.CallbackRoutesSetting,
	dispatch callbackDispatchers,
	rateLimit gin.HandlerFunc,
) gin.HandlerFunc {
	// serve 先应用限流；被限流（已 Abort）时不再调用处理函数。
	serve := func(c *gin.Context, handler gin.HandlerFunc) {
		if rateLimit != nil {
			rateLimit(c)
			if c.IsAborted() {
				return
			}
		}
		handler(c)
	}
	return func(c *gin.Context) {
		path := strings.TrimRight(c.Request.URL.Path, "/")
		method := c.Request.Method

		// 快速过滤：只处理以 /api/ 开头的请求
		if !strings.HasPrefix(path, "/api/") {
			c.Next()
			return
		}

		// 快速过滤：回调只用 POST 和 GET
		if method != http.MethodPost && method != http.MethodGet {
			c.Next()
			return
		}

		routes := getRoutes()
		if routes == nil {
			// 未配置自定义路由，放行使用默认路由
			c.Next()
			return
		}

		// 匹配自定义回调路径
		switch path {
		case routes.PaymentCallback:
			if routes.PaymentCallback != "" && (method == http.MethodPost || method == http.MethodGet) && dispatch.payment != nil {
				serve(c, dispatch.payment)
				c.Abort()
				return
			}
		case routes.DujiaoPayWebhook:
			if routes.DujiaoPayWebhook != "" && method == http.MethodPost && dispatch.dujiaoPay != nil {
				serve(c, dispatch.dujiaoPay)
				c.Abort()
				return
			}
		case routes.PaypalWebhook:
			if routes.PaypalWebhook != "" && method == http.MethodPost && dispatch.paypal != nil {
				serve(c, dispatch.paypal)
				c.Abort()
				return
			}
		case routes.StripeWebhook:
			if routes.StripeWebhook != "" && method == http.MethodPost && dispatch.stripe != nil {
				serve(c, dispatch.stripe)
				c.Abort()
				return
			}
		case routes.UpstreamCallback:
			if routes.UpstreamCallback != "" && method == http.MethodPost && dispatch.upstream != nil {
				serve(c, dispatch.upstream)
				c.Abort()
				return
			}
		}

		// 屏蔽默认回调路径（当对应的自定义路由已配置时）
		if defaultCallbackPaths[path] {
			shouldBlock := false
			switch path {
			case constants.DefaultPaymentCallbackPath:
				shouldBlock = routes.PaymentCallback != ""
			case constants.DefaultDujiaoPayWebhookPath:
				shouldBlock = routes.DujiaoPayWebhook != ""
			case constants.DefaultPaypalWebhookPath:
				shouldBlock = routes.PaypalWebhook != ""
			case constants.DefaultStripeWebhookPath:
				shouldBlock = routes.StripeWebhook != ""
			case constants.DefaultUpstreamCallbackPath:
				shouldBlock = routes.UpstreamCallback != ""
			}
			if shouldBlock {
				c.AbortWithStatus(http.StatusNotFound)
				return
			}
		}

		c.Next()
	}
}
