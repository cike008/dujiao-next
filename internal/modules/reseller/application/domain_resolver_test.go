package application

import (
	"context"
	"errors"
	"github.com/dujiao-next/internal/cache"
	"net/http"
	"os"
	"strconv"
	"testing"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	"github.com/dujiao-next/internal/config"
)

type resellerResolverRepoStub struct {
	domain *resellerdomain.Domain
	err    error
	calls  int
}

func (s *resellerResolverRepoStub) FindActiveVerifiedDomain(host string) (*resellerdomain.Domain, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.domain, nil
}

func TestResellerDomainResolverDisabledTreatsEmptyHostAsMain(t *testing.T) {
	repo := &resellerResolverRepoStub{}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: false})
	tenant, err := resolver.ResolveHost(context.Background(), "")
	if err != nil {
		t.Fatalf("disabled resolver should not fail on empty host: %v", err)
	}
	if !tenant.IsMain || tenant.Unavailable || tenant.ResellerID != nil {
		t.Fatalf("disabled resolver should return main tenant for empty host, got %+v", tenant)
	}
	if repo.calls != 0 {
		t.Fatalf("disabled resolver should not query repo, calls=%d", repo.calls)
	}
}

func TestResellerDomainResolverMainHost(t *testing.T) {
	repo := &resellerResolverRepoStub{}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "MAIN.example.test:443")
	if err != nil {
		t.Fatalf("resolve main host failed: %v", err)
	}
	if !tenant.IsMain || tenant.Unavailable {
		t.Fatalf("expected main tenant, got %+v", tenant)
	}
	if repo.calls != 0 {
		t.Fatalf("main host should not query repo, calls=%d", repo.calls)
	}
}

func TestResellerDomainResolverActiveDomain(t *testing.T) {
	id := uint(7)
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ID:         11,
		ResellerID: id,
		Domain:     "shop.example.test",
		Profile:    &resellerdomain.Profile{ID: id, UserID: 88, Status: resellerdomain.ProfileStatusActive},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "shop.example.test")
	if err != nil {
		t.Fatalf("resolve reseller host failed: %v", err)
	}
	if tenant.IsMain || tenant.ResellerID == nil || *tenant.ResellerID != id {
		t.Fatalf("expected reseller tenant, got %+v", tenant)
	}
	if tenant.ResellerUserID != 88 {
		t.Fatalf("expected reseller user id 88, got %d", tenant.ResellerUserID)
	}
}

func TestResellerDomainResolverInactiveProfileUnavailable(t *testing.T) {
	id := uint(7)
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ID:         11,
		ResellerID: id,
		Domain:     "shop.example.test",
		Status:     resellerdomain.DomainStatusActive,
		Profile:    &resellerdomain.Profile{ID: id, UserID: 88, Status: resellerdomain.ProfileStatusDisabled},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "shop.example.test")
	if err != nil {
		t.Fatalf("resolve disabled profile host failed: %v", err)
	}
	if !tenant.Unavailable || tenant.ResellerID != nil {
		t.Fatalf("expected disabled profile to be unavailable, got %+v", tenant)
	}
}

func TestResellerDomainResolverRequestUsesTrustedForwardedHost(t *testing.T) {
	id := uint(8)
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ID:         12,
		ResellerID: id,
		Domain:     "shop.example.test",
		Profile:    &resellerdomain.Profile{ID: id, UserID: 89, Status: resellerdomain.ProfileStatusActive},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{
		Enabled:              true,
		MainHosts:            []string{"main.example.test"},
		TrustedForwardedHost: true,
	})
	req, err := http.NewRequest(http.MethodGet, "https://internal.example.test/api/v1/public/config", nil)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	req.Host = "internal.example.test"
	req.Header.Set("X-Forwarded-Host", "shop.example.test")
	tenant, err := resolver.ResolveRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("resolve request failed: %v", err)
	}
	if tenant.ResellerID == nil || *tenant.ResellerID != id {
		t.Fatalf("expected forwarded reseller tenant, got %+v", tenant)
	}
	if repo.calls != 1 {
		t.Fatalf("expected one repo lookup for forwarded host, calls=%d", repo.calls)
	}
}

func TestResellerDomainResolverUnknownDomainUnavailable(t *testing.T) {
	repo := &resellerResolverRepoStub{}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	tenant, err := resolver.ResolveHost(context.Background(), "unknown.example.test")
	if err != nil {
		t.Fatalf("unknown domain should not return technical error: %v", err)
	}
	if !tenant.Unavailable || tenant.UnavailableReason != DomainUnavailableNotFound {
		t.Fatalf("expected unavailable not_found, got %+v", tenant)
	}
}

func TestResellerDomainResolverRepoError(t *testing.T) {
	repo := &resellerResolverRepoStub{err: errors.New("db down")}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true, MainHosts: []string{"main.example.test"}})
	_, err := resolver.ResolveHost(context.Background(), "shop.example.test")
	if err == nil {
		t.Fatal("expected repository error")
	}
}

func TestResellerDomainResolverRechecksPreviouslyActiveHost(t *testing.T) {
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ResellerID: 7, Domain: "recheck.example.test",
		Profile: &resellerdomain.Profile{ID: 7, UserID: 88, Status: resellerdomain.ProfileStatusActive},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true})
	ctx := context.Background()
	if tenant, err := resolver.ResolveHost(ctx, repo.domain.Domain); err != nil || !tenant.IsReseller() {
		t.Fatalf("active host rejected: %+v %v", tenant, err)
	}
	repo.domain.Profile.Status = resellerdomain.ProfileStatusDisabled
	if tenant, err := resolver.ResolveHost(ctx, repo.domain.Domain); err != nil || !tenant.Unavailable {
		t.Fatalf("disabled host accepted: %+v %v", tenant, err)
	}
	repo.domain.Profile.Status = resellerdomain.ProfileStatusActive
	repo.err = errors.New("database unavailable")
	if tenant, err := resolver.ResolveHost(ctx, repo.domain.Domain); err == nil || tenant.IsReseller() {
		t.Fatalf("database failure authorized tenant: %+v %v", tenant, err)
	}
	if repo.calls != 3 {
		t.Fatalf("expected authoritative lookup on every request, got %d", repo.calls)
	}
}

func TestResellerDomainResolverIgnoresStaleRedisAuthorization(t *testing.T) {
	portText := os.Getenv("TEST_RESELLER_REDIS_PORT")
	if portText == "" {
		t.Skip("TEST_RESELLER_REDIS_PORT is not set")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if err := cache.InitRedis(&config.RedisConfig{Enabled: true, Host: "127.0.0.1", Port: port, Prefix: "reseller-revocation-test"}); err != nil {
		t.Fatal(err)
	}
	client := cache.Client()
	t.Cleanup(func() { _ = client.Close(); _ = cache.InitRedis(nil) })
	ctx := context.Background()
	host := "stale-cache.example.test"
	t.Cleanup(func() { _ = cache.DelResellerDomain(ctx, host) })
	if err := cache.SetResellerDomain(ctx, host, cache.ResellerDomainCacheValue{ResellerID: 7, ResellerUserID: 88, PrimaryDomain: host}); err != nil {
		t.Fatal(err)
	}
	var cached cache.ResellerDomainCacheValue
	if hit, err := cache.GetResellerDomain(ctx, host, &cached); err != nil || !hit {
		t.Fatalf("stale cache fixture missing: hit=%v err=%v", hit, err)
	}
	repo := &resellerResolverRepoStub{domain: &resellerdomain.Domain{
		ResellerID: 7, Domain: host,
		Profile: &resellerdomain.Profile{ID: 7, UserID: 88, Status: resellerdomain.ProfileStatusDisabled},
	}}
	resolver := NewDomainResolver(repo, config.ResellerConfig{Enabled: true})
	if tenant, err := resolver.ResolveHost(ctx, host); err != nil || !tenant.Unavailable || tenant.IsReseller() {
		t.Fatalf("stale Redis authorization bypassed suspension: %+v %v", tenant, err)
	}
	if repo.calls != 1 {
		t.Fatalf("database state not checked: calls=%d", repo.calls)
	}
}
