package paymentbootstrap

import (
	"testing"
	"time"

	"github.com/dujiao-next/internal/shared/jsonmap"
)

func newTestThrottle(window time.Duration) (*exceptionAlertThrottle, *time.Time) {
	now := time.Date(2026, 10, 20, 6, 15, 0, 0, time.UTC)
	t := newExceptionAlertThrottle(window)
	t.now = func() time.Time { return now }
	return t, &now
}

func scanAlert(alertType, provider string) jsonmap.JSON {
	return jsonmap.JSON{"alert_type": alertType, "provider": provider, "message": "payment channel not found", "scan_rejected": true}
}

func TestScanFloodCollapsesIntoOneAlertPerWindow(t *testing.T) {
	throttle, now := newTestThrottle(10 * time.Minute)
	sent := 0
	for i := 0; i < 28; i++ {
		if ok, _ := throttle.allow(scanAlert("stripe_webhook_handle_failed", "stripe")); ok {
			sent++
		}
		*now = now.Add(time.Second)
	}
	if sent != 1 {
		t.Fatalf("a flood of identical scan alerts must produce one notification, got %d", sent)
	}
}

func TestScanCannotSuppressBusinessFailure(t *testing.T) {
	throttle, _ := newTestThrottle(10 * time.Minute)
	scan := scanAlert("stripe_webhook_handle_failed", "stripe")
	throttle.allow(scan)
	for i := 0; i < 3; i++ {
		failure := jsonmap.JSON{"alert_type": "stripe_webhook_handle_failed", "provider": "stripe", "message": "database unavailable"}
		if ok, _ := throttle.allow(failure); !ok {
			t.Fatal("an unclassified business failure must always be sent")
		}
	}
}

func TestScanChannelsAreIndependent(t *testing.T) {
	throttle, _ := newTestThrottle(10 * time.Minute)
	for _, id := range []uint{1, 2} {
		data := scanAlert("stripe_webhook_handle_failed", "stripe")
		data["channel_id"] = id
		if ok, _ := throttle.allow(data); !ok {
			t.Fatal("different channels must not suppress one another")
		}
	}
}

func TestSuppressedCountIsReportedWhenWindowReopens(t *testing.T) {
	throttle, now := newTestThrottle(10 * time.Minute)
	throttle.allow(scanAlert("callback_unrecognized", ""))
	for i := 0; i < 5; i++ {
		if ok, _ := throttle.allow(scanAlert("callback_unrecognized", "")); ok {
			t.Fatal("alerts inside the window must be suppressed")
		}
	}
	*now = now.Add(11 * time.Minute)
	ok, suppressed := throttle.allow(scanAlert("callback_unrecognized", ""))
	if !ok || suppressed != 5 {
		t.Fatalf("expected the next alert with suppressed=5, got ok=%v suppressed=%d", ok, suppressed)
	}
}

func TestDifferentAlertTypesAndProvidersAreIndependent(t *testing.T) {
	throttle, _ := newTestThrottle(10 * time.Minute)
	for _, a := range []jsonmap.JSON{
		scanAlert("stripe_webhook_handle_failed", "stripe"),
		scanAlert("paypal_webhook_handle_failed", "paypal"),
		scanAlert("dujiaopay_webhook_handle_failed", "dujiaopay"),
		scanAlert("callback_unrecognized", ""),
	} {
		if ok, _ := throttle.allow(a); !ok {
			t.Fatalf("first alert of each kind must be sent: %v", a)
		}
	}
}

func TestRealFailuresForDifferentPaymentsAreNeverMerged(t *testing.T) {
	throttle, _ := newTestThrottle(10 * time.Minute)
	first := jsonmap.JSON{"alert_type": "epay_callback_handle_failed", "provider": "epay", "payment_id": "101", "out_trade_no": "A1"}
	second := jsonmap.JSON{"alert_type": "epay_callback_handle_failed", "provider": "epay", "payment_id": "102", "out_trade_no": "A2"}
	if ok, _ := throttle.allow(first); !ok {
		t.Fatal("first real failure must be sent")
	}
	if ok, _ := throttle.allow(second); !ok {
		t.Fatal("a real failure for a different payment must not be hidden by another one")
	}
	if ok, _ := throttle.allow(first); !ok {
		t.Fatal("business failures must not be suppressed")
	}
}

func TestNilOrZeroWindowThrottleNeverSuppresses(t *testing.T) {
	var nilThrottle *exceptionAlertThrottle
	if ok, _ := nilThrottle.allow(scanAlert("x", "y")); !ok {
		t.Fatal("a nil throttle must let everything through")
	}
	zero := newExceptionAlertThrottle(0)
	for i := 0; i < 3; i++ {
		if ok, _ := zero.allow(scanAlert("x", "y")); !ok {
			t.Fatal("a zero window disables throttling")
		}
	}
}

func TestThrottleMemoryIsBoundedAndNeverDropsAlerts(t *testing.T) {
	throttle, _ := newTestThrottle(10 * time.Minute)
	for i := 0; i < exceptionAlertMaxKeys+50; i++ {
		data := jsonmap.JSON{"alert_type": "epay_callback_handle_failed", "payment_id": i}
		if ok, _ := throttle.allow(data); !ok {
			t.Fatalf("distinct alerts must always be sent, dropped at %d", i)
		}
	}
	if len(throttle.entries) > exceptionAlertMaxKeys {
		t.Fatalf("throttle entries must stay bounded, got %d", len(throttle.entries))
	}
}
