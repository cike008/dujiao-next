package requestmeta

import (
	"context"
	"testing"
)

func TestDeviceHint(t *testing.T) {
	for _, tc := range []struct {
		ua     string
		mobile bool
	}{
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X)", true},
		{"Mozilla/5.0 (Linux; Android 15) Chrome Mobile", true},
		{"Mozilla/5.0 (iPad; CPU OS 18_0)", true},
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X) Safari", false},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64)", false},
		{"", false},
	} {
		if got := IsMobile(WithUserAgent(context.Background(), tc.ua)); got != tc.mobile {
			t.Fatalf("%q: mobile=%v", tc.ua, got)
		}
	}
	if IsMobile(nil) {
		t.Fatal("nil context must default to desktop")
	}
}
