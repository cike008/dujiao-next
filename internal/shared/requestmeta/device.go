package requestmeta

import (
	"context"
	"strings"
)

type mobileKey struct{}

// WithUserAgent carries a presentation hint only; it must never authorize requests.
func WithUserAgent(ctx context.Context, userAgent string) context.Context {
	ua := strings.ToLower(userAgent)
	mobile := false
	for _, token := range []string{"iphone", "ipad", "ipod", "android", "mobile", "windows phone"} {
		if strings.Contains(ua, token) {
			mobile = true
			break
		}
	}
	return context.WithValue(ctx, mobileKey{}, mobile)
}

func IsMobile(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(mobileKey{}).(bool)
	return value
}
