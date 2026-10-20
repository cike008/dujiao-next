package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/constants"
	settingsintegration "github.com/dujiao-next/internal/modules/settings/schema/integration"

	"github.com/gin-gonic/gin"
)

func TestResolveAllowedOrigin(t *testing.T) {
	got := resolveAllowedOrigin("https://example.com", []string{"*"}, false)
	if got != "*" {
		t.Fatalf("wildcard without credentials should return *, got %s", got)
	}

	got = resolveAllowedOrigin("https://example.com", []string{"*"}, true)
	if got != "https://example.com" {
		t.Fatalf("wildcard with credentials should echo origin, got %s", got)
	}

	got = resolveAllowedOrigin("https://a.example.com", []string{"https://a.example.com", "https://b.example.com"}, false)
	if got != "https://a.example.com" {
		t.Fatalf("allow-list should return matched origin, got %s", got)
	}

	got = resolveAllowedOrigin("https://x.example.com", []string{"https://a.example.com"}, false)
	if got != "" {
		t.Fatalf("unmatched origin should be empty, got %s", got)
	}
}

func TestRequestIDMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RequestIDMiddleware())
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"request_id": getRequestID(c)})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(requestIDHeader, "req-123")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	if w.Header().Get(requestIDHeader) != "req-123" {
		t.Fatalf("response request id want req-123 got %s", w.Header().Get(requestIDHeader))
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp["request_id"] != "req-123" {
		t.Fatalf("context request id want req-123 got %s", resp["request_id"])
	}

	w2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w2, req2)
	generated := w2.Header().Get(requestIDHeader)
	if generated == "" {
		t.Fatalf("generated request id should not be empty")
	}
	if resp := strings.TrimSpace(generated); resp == "" {
		t.Fatalf("generated request id should not be blank")
	}
}

func TestJWTAuthMiddlewareMissingSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(JWTAuthMiddleware("", nil))
	r.GET("/admin/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/ping", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	var resp struct {
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if resp.StatusCode != 401 {
		t.Fatalf("status_code want 401 got %d", resp.StatusCode)
	}
}

func newCallbackTestEngine(routes *settingsintegration.CallbackRoutesSetting, limit gin.HandlerFunc, paid *int, next *int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := func(c *gin.Context) { *paid++; c.String(http.StatusOK, "handled") }
	engine := gin.New()
	engine.Use(newCallbackRouteMiddleware(
		func() *settingsintegration.CallbackRoutesSetting { return routes },
		callbackDispatchers{payment: handler, stripe: handler},
		limit,
	))
	engine.NoRoute(func(c *gin.Context) { *next++; c.String(http.StatusOK, "fell-through") })
	return engine
}

func callbackRequest(engine *gin.Engine, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

// 限流器：前 allowed 次放行，之后返回 429 并中止。
func countingLimiter(allowed int) gin.HandlerFunc {
	seen := 0
	return func(c *gin.Context) {
		seen++
		if seen > allowed {
			c.AbortWithStatus(http.StatusTooManyRequests)
			return
		}
		c.Next()
	}
}

func TestCustomCallbackPathsAreRateLimited(t *testing.T) {
	var handled, fell int
	routes := &settingsintegration.CallbackRoutesSetting{PaymentCallback: "/api/v1/custom/pay", StripeWebhook: "/api/v1/custom/stripe"}
	engine := newCallbackTestEngine(routes, countingLimiter(2), &handled, &fell)

	for i := 0; i < 2; i++ {
		if rec := callbackRequest(engine, http.MethodPost, "/api/v1/custom/pay"); rec.Code != http.StatusOK {
			t.Fatalf("request %d should pass the limiter, got %d", i+1, rec.Code)
		}
	}
	if rec := callbackRequest(engine, http.MethodPost, "/api/v1/custom/pay"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("third request must be rate limited, got %d", rec.Code)
	}
	if handled != 2 {
		t.Fatalf("handler must not run for rate-limited requests, ran %d times", handled)
	}
	// 限流计数在所有自定义入口之间共享。
	if rec := callbackRequest(engine, http.MethodPost, "/api/v1/custom/stripe"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("other custom paths share the same limiter, got %d", rec.Code)
	}
}

func TestCustomCallbackRespectsMethodAndTrailingSlash(t *testing.T) {
	var handled, fell int
	routes := &settingsintegration.CallbackRoutesSetting{StripeWebhook: "/api/v1/custom/stripe"}
	engine := newCallbackTestEngine(routes, nil, &handled, &fell)

	if rec := callbackRequest(engine, http.MethodPost, "/api/v1/custom/stripe/"); rec.Code != http.StatusOK || handled != 1 {
		t.Fatalf("POST with trailing slash should dispatch, code=%d handled=%d", rec.Code, handled)
	}
	if rec := callbackRequest(engine, http.MethodGet, "/api/v1/custom/stripe"); rec.Code != http.StatusOK || handled != 1 || fell != 1 {
		t.Fatalf("GET to a POST-only webhook must fall through, code=%d handled=%d fell=%d", rec.Code, handled, fell)
	}
}

func TestDefaultCallbackPathIsHiddenWhenCustomRouteConfigured(t *testing.T) {
	var handled, fell int
	routes := &settingsintegration.CallbackRoutesSetting{PaymentCallback: "/api/v1/custom/pay"}
	engine := newCallbackTestEngine(routes, countingLimiter(100), &handled, &fell)

	rec := callbackRequest(engine, http.MethodPost, constants.DefaultPaymentCallbackPath)
	if rec.Code != http.StatusNotFound || handled != 0 || fell != 0 {
		t.Fatalf("default path must be hidden, code=%d handled=%d fell=%d", rec.Code, handled, fell)
	}
}

func TestCallbackMiddlewareIgnoresUnrelatedRequests(t *testing.T) {
	var handled, fell int
	engine := newCallbackTestEngine(nil, countingLimiter(0), &handled, &fell)

	if rec := callbackRequest(engine, http.MethodPost, "/api/v1/payments/callback"); rec.Code != http.StatusOK || fell != 1 {
		t.Fatalf("without custom routes the default route must work untouched, code=%d fell=%d", rec.Code, fell)
	}
	routes := &settingsintegration.CallbackRoutesSetting{PaymentCallback: "/api/v1/custom/pay"}
	engine = newCallbackTestEngine(routes, countingLimiter(0), &handled, &fell)
	if rec := callbackRequest(engine, http.MethodPost, "/not-api/custom/pay"); rec.Code != http.StatusOK {
		t.Fatalf("non-API paths are not touched, got %d", rec.Code)
	}
	if rec := callbackRequest(engine, http.MethodPost, "/api/v1/other"); rec.Code != http.StatusOK {
		t.Fatalf("unrelated API paths are not rate limited by this middleware, got %d", rec.Code)
	}
}
