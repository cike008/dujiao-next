package middleware

import (
	"encoding/base64"
	"fmt"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestKeyByIPAndJSONField(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/auth", strings.NewReader(`{"email":" Test@Example.com "}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.RemoteAddr = "1.2.3.4:5678"

	key := KeyByIPAndJSONField("email")(c)
	if key != "test@example.com|1.2.3.4" {
		t.Fatalf("key want test@example.com|1.2.3.4 got %s", key)
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		t.Fatalf("read body after key extraction failed: %v", err)
	}
	if !strings.Contains(string(body), "Test@Example.com") {
		t.Fatalf("request body should be restored after reading field")
	}
}

func TestRateLimitMiddlewareWithoutClient(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(RateLimitMiddleware(nil, RateLimitRule{WindowSeconds: 60, MaxRequests: 1}, KeyByIP))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status want 200 got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"ok":true`) {
		t.Fatalf("expected handler response body, got %s", w.Body.String())
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request must be limited even without Redis: status want 429 got %d", w.Code)
	}
}

func TestLocalRateLimiterCapacityDoesNotEvictLiveEntries(t *testing.T) {
	now := time.Now()
	limiter := &localRateLimiter{entries: make(map[string]localRateLimitEntry, localRateLimitMaxEntries)}
	for i := 0; i < localRateLimitMaxEntries; i++ {
		limiter.entries[fmt.Sprintf("live-%d", i)] = localRateLimitEntry{
			count:     1,
			expiresAt: now.Add(10 * time.Minute),
		}
	}

	rule := RateLimitRule{WindowSeconds: 60, MaxRequests: 5}
	count, _, warned := limiter.increment("new-key", rule, now)
	if count <= int64(rule.MaxRequests) {
		t.Fatalf("new key must fail closed when local limiter is at capacity, count=%d", count)
	}
	if !warned {
		t.Fatal("first capacity rejection must request an operator warning")
	}
	_, _, warned = limiter.increment("another-new-key", rule, now.Add(time.Second))
	if warned {
		t.Fatal("capacity warning must be throttled")
	}
	_, _, warned = limiter.increment("later-new-key", rule, now.Add(localRateLimitWarningInterval))
	if !warned {
		t.Fatal("capacity warning should be emitted again after the throttle interval")
	}
	if len(limiter.entries) != localRateLimitMaxEntries {
		t.Fatalf("live entry count changed: got %d want %d", len(limiter.entries), localRateLimitMaxEntries)
	}
	if _, ok := limiter.entries["live-0"]; !ok {
		t.Fatal("capacity handling must not evict live counters")
	}
	if _, ok := limiter.entries["new-key"]; ok {
		t.Fatal("rejected new key must not be stored")
	}
}

func TestLocalRateLimiterPurgesExpiredEntriesBeforeRejectingNewKey(t *testing.T) {
	now := time.Now()
	limiter := &localRateLimiter{entries: make(map[string]localRateLimitEntry, localRateLimitMaxEntries)}
	for i := 0; i < localRateLimitMaxEntries; i++ {
		expiresAt := now.Add(time.Minute)
		if i == 0 {
			expiresAt = now.Add(-time.Second)
		}
		limiter.entries[fmt.Sprintf("entry-%d", i)] = localRateLimitEntry{
			count:     1,
			expiresAt: expiresAt,
		}
	}

	count, _, warned := limiter.increment("new-key", RateLimitRule{WindowSeconds: 60, MaxRequests: 5}, now)
	if count != 1 {
		t.Fatalf("new key count = %d, want 1 after expired entry cleanup", count)
	}
	if warned {
		t.Fatal("successful expired-entry cleanup must not emit a capacity warning")
	}
	if _, ok := limiter.entries["new-key"]; !ok {
		t.Fatal("new key should be stored after expired entry cleanup")
	}
	if _, ok := limiter.entries["entry-0"]; ok {
		t.Fatal("expired entry should be removed")
	}
}

func TestRateLimitMiddlewareFallsBackLocallyWhenRedisIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local address: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("close reserved address: %v", err)
	}
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
		MaxRetries:   -1,
	})
	t.Cleanup(func() { _ = client.Close() })

	r := gin.New()
	r.Use(RateLimitMiddleware(client, RateLimitRule{
		Prefix:        "redis-fallback-test",
		WindowSeconds: 60,
		MaxRequests:   1,
	}, KeyByIP))
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	first := httptest.NewRecorder()
	r.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first fallback request status = %d, want 200", first.Code)
	}

	second := httptest.NewRecorder()
	r.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/ping", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second fallback request status = %d, want 429", second.Code)
	}
}

func TestToInt64(t *testing.T) {
	cases := []struct {
		name  string
		input interface{}
		want  int64
		ok    bool
	}{
		{name: "int64", input: int64(10), want: 10, ok: true},
		{name: "int", input: int(11), want: 11, ok: true},
		{name: "uint8", input: uint8(12), want: 12, ok: true},
		{name: "float64", input: float64(13.9), want: 13, ok: true},
		{name: "string", input: "bad", want: 0, ok: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := toInt64(tc.input)
			if ok != tc.ok {
				t.Fatalf("ok want %v got %v", tc.ok, ok)
			}
			if got != tc.want {
				t.Fatalf("value want %d got %d", tc.want, got)
			}
		})
	}
}

func newGuestGuardForTest(client *redis.Client, now func() time.Time) *guestLookupGuard {
	return &guestLookupGuard{client: client, secret: "test-secret", now: now,
		rule: RateLimitRule{Prefix: "test:guest", WindowSeconds: 300, MaxRequests: 10, BlockSeconds: 60}}
}

func guestGuardRouter(g *guestLookupGuard) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(g.handle)
	r.GET("/:outcome", func(c *gin.Context) {
		if c.Param("outcome") == "miss" {
			ginutil.MarkGuestLookupFailure(c)
		}
		c.String(http.StatusOK, "lookup result")
	})
	return r
}

func guestGuardRequest(r http.Handler, outcome, email, ip, password string, tenant reseller.TenantContext) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/"+outcome, nil)
	req.RemoteAddr = ip + ":1234"
	req.Header.Set("Authorization", "Guest "+base64.RawURLEncoding.EncodeToString([]byte(email+"\n"+password)))
	req = req.WithContext(reseller.WithTenantContext(req.Context(), tenant))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestGuestFailureLimitAcrossIPsAndCooldown(t *testing.T) {
	now := time.Now()
	g := newGuestGuardForTest(nil, func() time.Time { return now })
	r := guestGuardRouter(g)
	tenant := reseller.MainTenantContext("shop.test")
	for i := 0; i < 10; i++ {
		w := guestGuardRequest(r, "miss", " Test@Example.com ", fmt.Sprintf("192.0.2.%d", i+1), strconv.Itoa(i), tenant)
		if w.Code != http.StatusOK {
			t.Fatalf("failure %d unexpectedly blocked: %d", i, w.Code)
		}
	}
	w := guestGuardRequest(r, "ok", "test@example.com", "198.51.100.1", "other-password", tenant)
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "60" || strings.Contains(w.Body.String(), "lookup result") {
		t.Fatalf("cross-IP lookup was not stopped before handler: %d %s", w.Code, w.Body.String())
	}
	if w := guestGuardRequest(r, "ok", "other@example.com", "198.51.100.1", "pass", tenant); w.Code != http.StatusOK {
		t.Fatal("other email blocked")
	}
	if w := guestGuardRequest(r, "ok", "test@example.com", "198.51.100.1", "pass", reseller.ResellerTenantContext("reseller.test", 1, 1, "reseller.test")); w.Code != http.StatusOK {
		t.Fatal("other tenant blocked")
	}
	now = now.Add(61 * time.Second)
	if w := guestGuardRequest(r, "ok", "test@example.com", "198.51.100.1", "pass", tenant); w.Code != http.StatusOK {
		t.Fatal("cooldown did not expire")
	}
}

func TestGuestFailureLimitOnlyCountsMarkedFailures(t *testing.T) {
	now := time.Now()
	g := newGuestGuardForTest(nil, func() time.Time { return now })
	r := guestGuardRouter(g)
	tenant := reseller.MainTenantContext("")
	for i := 0; i < 100; i++ {
		for _, outcome := range []string{"ok", "database-error"} {
			if w := guestGuardRequest(r, outcome, "test@example.com", "192.0.2.1", "pass", tenant); w.Code != http.StatusOK {
				t.Fatal("successful lookup or unmarked error counted")
			}
		}
	}
	if len(g.local.entries) != 0 {
		t.Fatal("normal queries allocated counters")
	}
	for i := 0; i < 9; i++ {
		guestGuardRequest(r, "miss", "test@example.com", "192.0.2.1", "bad", tenant)
	}
	guestGuardRequest(r, "ok", "test@example.com", "192.0.2.1", "known", tenant)
	guestGuardRequest(r, "miss", "test@example.com", "192.0.2.1", "bad", tenant)
	if w := guestGuardRequest(r, "ok", "test@example.com", "192.0.2.1", "pass", tenant); w.Code != http.StatusTooManyRequests {
		t.Fatal("successful lookup reset the shared failure budget")
	}
}

func TestGuestFailureLocalWindowCapacityAndConcurrency(t *testing.T) {
	now := time.Now()
	g := newGuestGuardForTest(nil, func() time.Time { return now })
	rule := g.rule
	rule.MaxRequests--
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); g.local.increment("key", rule, now) }()
	}
	wg.Wait()
	if count, _ := g.localCount("key", now); count != 10 {
		t.Fatalf("concurrent failure count: %d", count)
	}
	if count, _ := g.localCount("key", now.Add(time.Minute)); count != 0 {
		t.Fatal("cooldown did not expire")
	}
	g.local.increment("window", rule, now)
	if count, _ := g.localCount("window", now.Add(300*time.Second)); count != 0 {
		t.Fatal("failure window did not expire")
	}
	for i := 0; i < localRateLimitMaxEntries; i++ {
		g.local.increment(strconv.Itoa(i), rule, now)
	}
	if count, _ := g.localCount("new", now); count < 10 {
		t.Fatal("capacity exhaustion failed open")
	}
}

func TestGuestFailureRedisUnavailableFallback(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:0", DialTimeout: 20 * time.Millisecond, MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	g := newGuestGuardForTest(client, time.Now)
	r := guestGuardRouter(g)
	for i := 0; i < 10; i++ {
		guestGuardRequest(r, "miss", "test@example.com", "192.0.2.1", "bad", reseller.MainTenantContext(""))
	}
	if w := guestGuardRequest(r, "ok", "test@example.com", "192.0.2.2", "pass", reseller.MainTenantContext("")); w.Code != http.StatusTooManyRequests {
		t.Fatal("Redis outage disabled protection")
	}
}

func TestGuestFailureKeyDoesNotExposeCredentialsOrSplitHostAliases(t *testing.T) {
	g := newGuestGuardForTest(nil, time.Now)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	c.Request = c.Request.WithContext(reseller.WithTenantContext(c.Request.Context(), reseller.MainTenantContext("one.test")))
	key := g.key(c, " Test@Example.com ")
	if strings.Contains(key, "example") || strings.Contains(key, "Test") {
		t.Fatal("email leaked into counter key")
	}
	c.Request = c.Request.WithContext(reseller.WithTenantContext(c.Request.Context(), reseller.MainTenantContext("two.test")))
	if key != g.key(c, "test@example.com") {
		t.Fatal("host alias or email casing bypassed shared counter")
	}
	g.secret = "another-secret"
	if key == g.key(c, "test@example.com") {
		t.Fatal("counter key is not secret-bound")
	}
}

func TestGuestFailureRedisSharedAcrossInstances(t *testing.T) {
	binary, err := exec.LookPath("redis-server")
	if err != nil {
		t.Skip("redis-server not installed; shared Redis test requires a local server")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	cmd := exec.Command(binary, "--bind", "127.0.0.1", "--port", strconv.Itoa(port), "--save", "", "--appendonly", "no", "--dir", t.TempDir())
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	client := redis.NewClient(&redis.Options{Addr: fmt.Sprintf("127.0.0.1:%d", port), MaxRetries: -1})
	t.Cleanup(func() { _ = client.Close() })
	deadline := time.Now().Add(5 * time.Second)
	for client.Ping(t.Context()).Err() != nil {
		if time.Now().After(deadline) {
			t.Fatal("Redis did not become ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	r1 := guestGuardRouter(newGuestGuardForTest(client, time.Now))
	r2 := guestGuardRouter(newGuestGuardForTest(client, time.Now))
	for i := 0; i < 10; i++ {
		r := r1
		if i%2 == 0 {
			r = r2
		}
		guestGuardRequest(r, "miss", "test@example.com", "192.0.2.1", "bad", reseller.MainTenantContext(""))
	}
	for _, r := range []*gin.Engine{r1, r2} {
		if w := guestGuardRequest(r, "ok", "test@example.com", "192.0.2.2", "pass", reseller.MainTenantContext("")); w.Code != http.StatusTooManyRequests {
			t.Fatal("Redis failure counters were not shared")
		}
	}
}
