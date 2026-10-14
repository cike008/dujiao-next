package application_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dujiao-next/internal/config"
	. "github.com/dujiao-next/internal/modules/order/application"
	resellerapplication "github.com/dujiao-next/internal/modules/reseller/application"
	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func TestResellerRequestHostControlsCheckoutAndOrderScope(t *testing.T) {
	f := newOrderResellerSnapshotFixture(t)
	if err := f.db.AutoMigrate(&resellerdomain.Domain{}); err != nil {
		t.Fatal(err)
	}
	second := resellerdomain.Profile{
		UserID: f.buyer.ID, Status: resellerdomain.ProfileStatusActive,
		MaxMarkupPercent: money.FromDecimal(decimal.NewFromInt(80)),
	}
	if err := f.db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	setting := resellerdomain.ProductSetting{
		ResellerID: second.ID, ProductID: f.product.ID, SKUID: f.sku.ID,
		IsListed: true, PricingMode: resellerdomain.PricingModeFixedPrice,
		FixedPriceAmount: money.FromDecimal(decimal.NewFromInt(160)),
	}
	if err := f.db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	suffix := fmt.Sprintf("%d.example.test", time.Now().UnixNano())
	firstHost, secondHost, mainHost := "first."+suffix, "second."+suffix, "main."+suffix
	for host, id := range map[string]uint{firstHost: f.profile.ID, secondHost: second.ID} {
		domain := resellerdomain.Domain{
			ResellerID: id, Domain: host, Type: resellerdomain.DomainTypeCustom,
			Status: resellerdomain.DomainStatusActive, VerificationStatus: resellerdomain.DomainVerificationVerified,
		}
		if err := f.db.Create(&domain).Error; err != nil {
			t.Fatal(err)
		}
	}
	resolver := resellerapplication.NewDomainResolver(f.resellerRepo, config.ResellerConfig{
		Enabled: true, MainHosts: []string{mainHost}, TrustedForwardedHost: false,
	})
	request := httptest.NewRequest("GET", "https://"+firstHost+"/api/v1/public/config", nil)
	request.Header.Set("X-Forwarded-Host", secondHost)
	firstTenant, err := resolver.ResolveRequest(context.Background(), request)
	if err != nil || !firstTenant.IsReseller() || *firstTenant.ResellerID != f.profile.ID {
		t.Fatalf("request host must take precedence over untrusted forwarded host: tenant=%+v err=%v", firstTenant, err)
	}
	request.Host = secondHost
	secondTenant, err := resolver.ResolveRequest(context.Background(), request)
	if err != nil || !secondTenant.IsReseller() || *secondTenant.ResellerID != second.ID {
		t.Fatalf("second domain resolved incorrectly: tenant=%+v err=%v", secondTenant, err)
	}
	input := f.createInput(f.owner.ID)
	input.Tenant = firstTenant
	preview, err := f.svc.PreviewOrder(input)
	if err != nil || !preview.TotalAmount.Decimal.Equal(decimal.NewFromInt(130)) {
		t.Fatalf("first checkout should use price 130: preview=%+v err=%v", preview, err)
	}
	order, err := f.svc.CreateOrder(input)
	if err != nil {
		t.Fatal(err)
	}
	if !order.TotalAmount.Decimal.Equal(preview.TotalAmount.Decimal) || order.ResellerID == nil || *order.ResellerID != f.profile.ID {
		t.Fatalf("created order does not match its host and preview: %+v", order)
	}
	input.Tenant = secondTenant
	preview, err = f.svc.PreviewOrder(input)
	if err != nil || !preview.TotalAmount.Decimal.Equal(decimal.NewFromInt(160)) {
		t.Fatalf("second checkout should use price 160: preview=%+v err=%v", preview, err)
	}
	if _, err := f.svc.GetOrderByUserOrderNoForTenant(secondTenant, order.OrderNo, f.owner.ID); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("second store must not read first store order: %v", err)
	}
	request.Host = mainHost
	mainTenant, err := resolver.ResolveRequest(context.Background(), request)
	if err != nil || !mainTenant.IsMain {
		t.Fatalf("main host resolution failed: tenant=%+v err=%v", mainTenant, err)
	}
	if _, err := f.svc.GetOrderByUserOrderNoForTenant(mainTenant, order.OrderNo, f.owner.ID); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("main site must not read reseller order: %v", err)
	}
	if _, err := f.svc.GetOrderByUserOrderNoForTenant(firstTenant, order.OrderNo, f.owner.ID); err != nil {
		t.Fatalf("origin store should read its own order: %v", err)
	}
	request.Host = "unknown." + suffix
	unknownTenant, err := resolver.ResolveRequest(context.Background(), request)
	if err != nil || !unknownTenant.Unavailable {
		t.Fatalf("unknown domain must be unavailable: tenant=%+v err=%v", unknownTenant, err)
	}
}
