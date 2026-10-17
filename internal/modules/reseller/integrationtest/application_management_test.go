package integrationtest

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	resellerapplication "github.com/dujiao-next/internal/modules/reseller/application"
	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"
	"strings"
	"testing"
	"time"

	resellergormstore "github.com/dujiao-next/internal/modules/reseller/infrastructure/gormstore"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	userdomain "github.com/dujiao-next/internal/modules/identity/user/domain"

	"github.com/dujiao-next/internal/config"
	admindomain "github.com/dujiao-next/internal/modules/identity/admin/domain"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func openResellerManagementServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:reseller_management_%d?mode=memory&cache=shared", time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db failed: %v", err)
	}
	if err := db.AutoMigrate(&userdomain.User{}, &admindomain.Admin{}, &resellerdomain.Profile{}, &resellerdomain.Domain{}, &resellerdomain.SiteConfig{}); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}
	return db
}

func seedResellerManagementUser(t *testing.T, db *gorm.DB, email string) userdomain.User {
	t.Helper()
	user := userdomain.User{Email: email, PasswordHash: "hash", DisplayName: email}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	return user
}

func newResellerManagementServiceForTest(db *gorm.DB) *ResellerManagementService {
	return NewResellerManagementService(resellergormstore.New(db), config.ResellerConfig{
		Enabled:          true,
		SelfApplyEnabled: true,
		SubdomainBase:    "shop.example.test",
		MainHosts:        []string{"main.example.test"},
	})
}

func TestResellerDisableRestorePreservesDomainAndBranding(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "lifecycle@example.test")
	svc := newResellerManagementServiceForTest(db)
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{}); err != nil {
		t.Fatal(err)
	}
	domain := resellerdomain.Domain{
		ResellerID: profile.ID, Domain: "lifecycle.customer.example", Type: resellerdomain.DomainTypeCustom,
		Status: resellerdomain.DomainStatusActive, VerificationStatus: resellerdomain.DomainVerificationVerified, IsPrimary: true,
	}
	if err := db.Create(&domain).Error; err != nil {
		t.Fatal(err)
	}
	store := resellergormstore.New(db)
	if _, err := store.UpsertSiteConfig(resellerdomain.SiteConfig{ResellerID: profile.ID}); err != nil {
		t.Fatal(err)
	}
	resolver := resellerapplication.NewDomainResolver(store, config.ResellerConfig{Enabled: true})
	assertAvailable := func(want bool) {
		t.Helper()
		tenant, err := resolver.ResolveHost(context.Background(), domain.Domain)
		if err != nil || tenant.IsReseller() != want || tenant.IsMain {
			t.Fatalf("availability=%v: tenant=%+v err=%v", want, tenant, err)
		}
	}
	assertAvailable(true)
	if _, err := svc.DisableProfile(9, profile.ID, "test suspension"); err != nil {
		t.Fatal(err)
	}
	assertAvailable(false)
	if _, err := svc.RestoreProfile(9, profile.ID); err != nil {
		t.Fatal(err)
	}
	assertAvailable(true)
	var domains int64
	if err := db.Model(&resellerdomain.Domain{}).Where("reseller_id = ?", profile.ID).Count(&domains).Error; err != nil || domains != 1 {
		t.Fatalf("domain records changed: count=%d err=%v", domains, err)
	}
	preserved, err := store.GetDomainByID(domain.ID)
	if err != nil || preserved.Status != domain.Status || !preserved.IsPrimary {
		t.Fatalf("domain binding changed: %+v err=%v", preserved, err)
	}
	if branding, err := store.GetSiteConfigByResellerID(profile.ID); err != nil || branding == nil {
		t.Fatalf("branding was removed: %+v err=%v", branding, err)
	}
}

func TestResellerManagementApplyCreatesPendingAndReapplyRejected(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "apply-reseller@example.test")
	svc := newResellerManagementServiceForTest(db)

	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "first application"})
	if err != nil {
		t.Fatalf("ApplyUserReseller failed: %v", err)
	}
	if profile.Status != resellerdomain.ProfileStatusPendingReview || profile.ApplyReason != "first application" {
		t.Fatalf("unexpected created profile: %+v", profile)
	}

	profile.Status = resellerdomain.ProfileStatusRejected
	profile.RejectReason = "missing info"
	reviewer := uint(7)
	now := time.Now()
	profile.ReviewedBy = &reviewer
	profile.ReviewedAt = &now
	if err := db.Save(profile).Error; err != nil {
		t.Fatalf("save rejected profile failed: %v", err)
	}

	reapplied, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "second application"})
	if err != nil {
		t.Fatalf("reapply failed: %v", err)
	}
	if reapplied.Status != resellerdomain.ProfileStatusPendingReview || reapplied.ApplyReason != "second application" || reapplied.RejectReason != "" || reapplied.ReviewedBy != nil || reapplied.ReviewedAt != nil {
		t.Fatalf("unexpected reapplied profile: %+v", reapplied)
	}
}

func TestResellerManagementApplyDisabledConfigRejects(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "apply-disabled@example.test")
	svc := NewResellerManagementService(resellergormstore.New(db), config.ResellerConfig{Enabled: false, SelfApplyEnabled: true})

	_, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "want access"})
	if !errors.Is(err, ErrResellerApplyDisabled) {
		t.Fatalf("expected ErrResellerApplyDisabled, got %v", err)
	}
	if _, err := svc.SubmitUserCustomDomain(user.ID, "shop.customer.example"); !errors.Is(err, ErrResellerApplyDisabled) {
		t.Fatalf("disabled reseller mode accepted domain application: %v", err)
	}
}

func TestResellerManagementApproveActivatesProfileWithoutAutoSubdomain(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "approve-reseller@example.test")
	svc := newResellerManagementServiceForTest(db)
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "approve me"})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	approved, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{
		DefaultMarkupPercent: decimal.NewFromInt(12),
		MaxMarkupPercent:     decimal.NewFromInt(45),
	})
	if err != nil {
		t.Fatalf("ApproveProfile failed: %v", err)
	}
	if approved.Profile.Status != resellerdomain.ProfileStatusActive || approved.Profile.ReviewedBy == nil || *approved.Profile.ReviewedBy != 9 {
		t.Fatalf("unexpected approved profile: %+v", approved.Profile)
	}
	if approved.SystemDomain != nil {
		t.Fatalf("expected approval to skip automatic system domain, got %+v", approved.SystemDomain)
	}
	var count int64
	if err := db.Model(&resellerdomain.Domain{}).Where("reseller_id = ?", profile.ID).Count(&count).Error; err != nil {
		t.Fatalf("count domains failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no domains after approval, got %d", count)
	}
}

func TestResellerManagementRejectsDefaultMarkupAboveMaxMarkup(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "markup-limit@example.test")
	svc := newResellerManagementServiceForTest(db)
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "approve me"})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	_, err = svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{
		DefaultMarkupPercent: decimal.NewFromInt(30),
		MaxMarkupPercent:     decimal.NewFromInt(10),
	})
	if !errors.Is(err, ErrResellerProfileStatusInvalid) {
		t.Fatalf("expected ErrResellerProfileStatusInvalid on approve, got %v", err)
	}

	approved, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{
		DefaultMarkupPercent: decimal.NewFromInt(10),
		MaxMarkupPercent:     decimal.NewFromInt(30),
	})
	if err != nil {
		t.Fatalf("approve valid profile failed: %v", err)
	}
	_, err = svc.UpdateProfileOperationalConfig(9, approved.Profile.ID, ResellerProfileUpdateInput{
		DefaultMarkupPercent: decimal.NewFromInt(40),
		MaxMarkupPercent:     decimal.NewFromInt(30),
		SettlementStatus:     resellerdomain.SettlementStatusNormal,
	})
	if !errors.Is(err, ErrResellerProfileStatusInvalid) {
		t.Fatalf("expected ErrResellerProfileStatusInvalid on update, got %v", err)
	}
}

func TestResellerManagementAssignSystemSubdomainCreatesAndEditsReadableDomain(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "assign-subdomain@example.test")
	svc := newResellerManagementServiceForTest(db)
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "approve me"})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if _, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{}); err != nil {
		t.Fatalf("approve profile failed: %v", err)
	}

	created, err := svc.AssignSystemSubdomain(context.Background(), 9, profile.ID, ResellerSystemDomainInput{Subdomain: "Hello"})
	if err != nil {
		t.Fatalf("AssignSystemSubdomain create failed: %v", err)
	}
	if created.Domain != "hello.shop.example.test" || created.Type != resellerdomain.DomainTypeSubdomain || created.Status != resellerdomain.DomainStatusActive || created.VerificationStatus != resellerdomain.DomainVerificationVerified || !created.IsPrimary || created.VerifiedAt == nil {
		t.Fatalf("unexpected created system domain: %+v", created)
	}

	updated, err := svc.AssignSystemSubdomain(context.Background(), 9, profile.ID, ResellerSystemDomainInput{Subdomain: "brand.shop.example.test"})
	if err != nil {
		t.Fatalf("AssignSystemSubdomain update failed: %v", err)
	}
	if updated.ID != created.ID || updated.Domain != "brand.shop.example.test" || !updated.IsPrimary {
		t.Fatalf("unexpected updated system domain: created=%+v updated=%+v", created, updated)
	}
	var domains []resellerdomain.Domain
	if err := db.Where("reseller_id = ? AND type = ?", profile.ID, resellerdomain.DomainTypeSubdomain).Find(&domains).Error; err != nil {
		t.Fatalf("list system domains failed: %v", err)
	}
	if len(domains) != 1 {
		t.Fatalf("expected one editable system domain, got %+v", domains)
	}
}

func TestResellerManagementSubmitCustomDomainRequiresActiveProfile(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "custom-domain@example.test")
	svc := newResellerManagementServiceForTest(db)
	if _, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "pending"}); err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	_, err := svc.SubmitUserCustomDomain(user.ID, "shop.customer.example")
	if !errors.Is(err, ErrResellerProfileInactive) {
		t.Fatalf("expected ErrResellerProfileInactive, got %v", err)
	}
}

func TestResellerManagementSubmitAndApproveCustomDomain(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "domain-approve@example.test")
	svc := newResellerManagementServiceForTest(db)
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "approve me"})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if _, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{}); err != nil {
		t.Fatalf("approve profile failed: %v", err)
	}

	for _, input := range []string{"gpt", "localhost", "127.0.0.1", "https://shop.customer.com", "Shop.Customer.Example:443", "*.customer.com", "shop.customer.com/path", "shop .customer.com", "-shop.customer.com", "shop..customer.com", strings.Repeat("a", 64) + ".customer.com"} {
		if _, err := svc.SubmitUserCustomDomain(user.ID, input); !errors.Is(err, resellercontract.ErrDomainInvalid) {
			t.Fatalf("invalid domain %q should be rejected, got %v", input, err)
		}
	}
	var count int64
	if err := db.Model(&resellerdomain.Domain{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("invalid domain submissions must not create bindings: count=%d err=%v", count, err)
	}
	domain, err := svc.SubmitUserCustomDomain(user.ID, "Shop.Customer.Example")
	if err != nil {
		t.Fatalf("SubmitUserCustomDomain failed: %v", err)
	}
	if domain.Domain != "shop.customer.example" || domain.Type != resellerdomain.DomainTypeCustom || domain.VerificationStatus != resellerdomain.DomainVerificationPending || domain.Status != resellerdomain.DomainStatusPendingReview {
		t.Fatalf("unexpected submitted domain: %+v", domain)
	}
	decoded, err := hex.DecodeString(strings.TrimPrefix(domain.VerificationToken, "moshshop-verification="))
	if err != nil || len(decoded) != 32 {
		t.Fatal("missing random ownership token")
	}
	if _, err := svc.SubmitUserCustomDomain(user.ID, domain.Domain); !errors.Is(err, resellercontract.ErrDomainConflict) {
		t.Fatalf("duplicate application accepted: %v", err)
	}
	stored, err := resellergormstore.New(db).GetDomainByID(domain.ID)
	if err != nil || stored.VerificationToken != domain.VerificationToken {
		t.Fatal("duplicate request rotated the DNS token")
	}
	second, err := svc.SubmitUserCustomDomain(user.ID, "other.customer.example")
	if err != nil || second.VerificationToken == domain.VerificationToken {
		t.Fatal("domain ownership tokens must be independent")
	}

	// 没有 TXT 记录、记录错误、DNS 查询失败时都不能通过审核。
	for name, resolver := range map[string]func(context.Context, string) ([]string, error){
		"no record": func(context.Context, string) ([]string, error) { return nil, nil },
		"wrong token": func(context.Context, string) ([]string, error) {
			return []string{"moshshop-verification=deadbeef"}, nil
		},
		"dns error":    func(context.Context, string) ([]string, error) { return nil, errors.New("lookup failed") },
		"other domain": func(_ context.Context, n string) ([]string, error) { return []string{n}, nil },
	} {
		svc.SetTXTResolver(resolver)
		if _, err := svc.ApproveDomain(context.Background(), 9, domain.ID); !errors.Is(err, resellercontract.ErrDomainVerificationFailed) {
			t.Fatalf("%s: approval must fail verification, got %v", name, err)
		}
		pending, err := resellergormstore.New(db).GetDomainByID(domain.ID)
		if err != nil || pending.Status != resellerdomain.DomainStatusPendingReview || pending.VerificationStatus != resellerdomain.DomainVerificationPending {
			t.Fatalf("%s: failed verification must leave the domain pending: %+v err=%v", name, pending, err)
		}
	}

	var queried string
	svc.SetTXTResolver(func(_ context.Context, name string) ([]string, error) {
		queried = name
		return []string{"v=spf1 -all", "\"" + domain.VerificationToken + "\""}, nil
	})
	approved, err := svc.ApproveDomain(context.Background(), 9, domain.ID)
	if err != nil {
		t.Fatalf("ApproveDomain failed: %v", err)
	}
	if queried != "_moshshop-verify.shop.customer.example" {
		t.Fatalf("unexpected TXT name queried: %q", queried)
	}
	if approved.Status != resellerdomain.DomainStatusActive || approved.VerificationStatus != resellerdomain.DomainVerificationVerified || approved.VerifiedAt == nil {
		t.Fatalf("unexpected approved domain: %+v", approved)
	}

	// 已验证的域名重新启用时不再重复查询 DNS。
	if _, err := svc.DisableDomain(context.Background(), 9, domain.ID); err != nil {
		t.Fatalf("DisableDomain failed: %v", err)
	}
	svc.SetTXTResolver(func(context.Context, string) ([]string, error) { return nil, errors.New("must not be called") })
	if _, err := svc.ApproveDomain(context.Background(), 9, domain.ID); err != nil {
		t.Fatalf("re-enabling a verified domain must not require DNS: %v", err)
	}
}

func TestResellerManagementSystemDomainRequestStaysPendingUntilApproval(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "platform-domain@example.test")
	svc := NewResellerManagementService(resellergormstore.New(db), config.ResellerConfig{
		Enabled: true, SelfApplyEnabled: true, SubdomainBase: "moshshop.com", SubdomainSuffix: "ai",
	})
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitUserSystemDomain(user.ID, "gpt"); !errors.Is(err, ErrResellerProfileInactive) {
		t.Fatalf("unapproved profile should be rejected: %v", err)
	}
	if _, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{}); err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"https://gpt", "gpt.moshshop.com", "gpt:443", "ai", "-gpt", "gpt-", "gpt name", "admin", "www"} {
		if _, err := svc.SubmitUserSystemDomain(user.ID, prefix); err == nil {
			t.Fatalf("invalid/reserved prefix accepted: %q", prefix)
		}
	}
	base, suffix := svc.SystemDomainPolicy()
	if base != "moshshop.com" || suffix != "ai" {
		t.Fatalf("unexpected policy %s %s", base, suffix)
	}
	row, err := svc.SubmitUserSystemDomain(user.ID, "GPT")
	if err != nil {
		t.Fatal(err)
	}
	if row.Domain != "gptai.moshshop.com" || row.Type != resellerdomain.DomainTypeSubdomain || row.Status != resellerdomain.DomainStatusPendingReview || row.VerificationStatus != resellerdomain.DomainVerificationPending || row.IsPrimary {
		t.Fatalf("request must stay pending: %+v", row)
	}
	repo := resellergormstore.New(db)
	if active, err := repo.FindActiveVerifiedDomain(row.Domain); err != nil || active != nil {
		t.Fatalf("pending domain must not resolve: %v %+v", err, active)
	}
	if _, err := svc.SubmitUserSystemDomain(user.ID, "gptai"); !errors.Is(err, resellercontract.ErrDomainConflict) {
		t.Fatalf("duplicate must conflict: %v", err)
	}
	approved, err := svc.ApproveDomain(context.Background(), 9, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if approved.Status != resellerdomain.DomainStatusActive || !approved.IsPrimary {
		t.Fatalf("admin approval failed: %+v", approved)
	}
	other := seedResellerManagementUser(t, db, "other-platform@example.test")
	otherProfile, err := svc.ApplyUserReseller(other.ID, ResellerApplyInput{Reason: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApproveProfile(context.Background(), 9, otherProfile.ID, ResellerApproveInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitUserSystemDomain(other.ID, "gpt"); !errors.Is(err, resellercontract.ErrDomainConflict) {
		t.Fatalf("other reseller must not claim bound domain: %v", err)
	}
}

func TestResellerManagementDisablePrimaryDomainPromotesNextActiveVerifiedDomain(t *testing.T) {
	db := openResellerManagementServiceTestDB(t)
	user := seedResellerManagementUser(t, db, "disable-primary@example.test")
	svc := newResellerManagementServiceForTest(db)
	profile, err := svc.ApplyUserReseller(user.ID, ResellerApplyInput{Reason: "approve me"})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	approved, err := svc.ApproveProfile(context.Background(), 9, profile.ID, ResellerApproveInput{})
	if err != nil {
		t.Fatalf("approve profile failed: %v", err)
	}
	now := time.Now()
	primary := resellerdomain.Domain{
		ResellerID:         approved.Profile.ID,
		Domain:             "primary.shop.example.test",
		Type:               resellerdomain.DomainTypeSubdomain,
		Status:             resellerdomain.DomainStatusActive,
		VerificationStatus: resellerdomain.DomainVerificationVerified,
		IsPrimary:          true,
		VerifiedAt:         &now,
	}
	secondary := resellerdomain.Domain{
		ResellerID:         approved.Profile.ID,
		Domain:             "secondary.shop.example.test",
		Type:               resellerdomain.DomainTypeSubdomain,
		Status:             resellerdomain.DomainStatusActive,
		VerificationStatus: resellerdomain.DomainVerificationVerified,
		IsPrimary:          false,
		VerifiedAt:         &now,
	}
	if err := db.Create(&primary).Error; err != nil {
		t.Fatalf("create primary domain failed: %v", err)
	}
	if err := db.Create(&secondary).Error; err != nil {
		t.Fatalf("create secondary domain failed: %v", err)
	}

	disabled, err := svc.DisableDomain(context.Background(), 9, primary.ID)
	if err != nil {
		t.Fatalf("disable primary domain failed: %v", err)
	}
	if disabled.IsPrimary {
		t.Fatalf("disabled domain should not remain primary: %+v", disabled)
	}
	var loadedSecondary resellerdomain.Domain
	if err := db.First(&loadedSecondary, secondary.ID).Error; err != nil {
		t.Fatalf("load secondary domain failed: %v", err)
	}
	if !loadedSecondary.IsPrimary {
		t.Fatalf("expected active verified secondary domain to become primary: %+v", loadedSecondary)
	}
}
