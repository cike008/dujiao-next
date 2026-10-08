package paymenthttp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderdomain "github.com/dujiao-next/internal/modules/order/domain"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	reseller "github.com/dujiao-next/internal/modules/reseller/contract"
	"github.com/dujiao-next/internal/platform/http/ginutil"
	"github.com/dujiao-next/internal/platform/http/response"

	"github.com/gin-gonic/gin"
)

type failedGuestPaymentLookup struct{ err error }

func (s failedGuestPaymentLookup) GetOrderByGuestOrderNoForTenant(reseller.TenantContext, string, string, string) (*orderdomain.Order, error) {
	return nil, s.err
}
func (s failedGuestPaymentLookup) GetOrderByGuestForTenant(reseller.TenantContext, uint, string, string) (*orderdomain.Order, error) {
	return nil, s.err
}

type guardedPaymentWriter struct{ called bool }

func (s *guardedPaymentWriter) GetPayment(uint) (*paymentdomain.Payment, error) {
	return &paymentdomain.Payment{ID: 1, OrderID: 1}, nil
}
func (s *guardedPaymentWriter) CreatePayment(CreatePaymentInput) (*CreatePaymentResult, error) {
	s.called = true
	return nil, errors.New("unexpected create")
}
func (s *guardedPaymentWriter) CapturePayment(CapturePaymentInput) (*paymentdomain.Payment, error) {
	s.called = true
	return nil, errors.New("unexpected capture")
}

func TestGuestPaymentLookupFailureSignals(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, action := range []string{"latest", "create", "capture"} {
		for _, unmatched := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/unmatched=%t", action, unmatched), func(t *testing.T) {
				err := errors.New("database failure")
				if unmatched {
					err = ErrGuestOrderNotFound
				}
				lookup := failedGuestPaymentLookup{err: err}
				writer := &guardedPaymentWriter{}
				writeHandler := &WriteHandler{guestOrders: lookup, payments: writer}
				latestHandler := &LatestHandler{guestOrders: lookup}
				marked := false
				r := gin.New()
				r.Use(func(c *gin.Context) { c.Next(); marked = ginutil.GuestLookupFailed(c) })
				r.GET("/latest", latestHandler.GetGuestLatestPayment)
				r.POST("/create", writeHandler.CreateGuestPayment)
				r.POST("/capture/:id", writeHandler.CaptureGuestPayment)
				method, path := http.MethodPost, "/create"
				if action == "latest" {
					method, path = http.MethodGet, "/latest?order_no=missing"
				}
				if action == "capture" {
					path = "/capture/1"
				}
				req := httptest.NewRequest(method, path, strings.NewReader(`{"order_no":"missing","channel_id":1}`))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Guest "+base64.RawURLEncoding.EncodeToString([]byte("test@example.com\nbad")))
				r.ServeHTTP(httptest.NewRecorder(), req)
				if marked != unmatched {
					t.Fatalf("failure marker=%v want %v", marked, unmatched)
				}
				if writer.called {
					t.Fatal("unmatched credentials triggered payment mutation")
				}
			})
		}
	}
}

func TestRespondPaymentCreateError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{
			name: "gateway response invalid",
			err:  ErrPaymentGatewayResponseInvalid,
			code: response.CodeBadRequest,
			msg:  "支付网关响应异常",
		},
		{
			name: "recharge channel not allowed",
			err:  ErrPaymentChannelNotAllowedForRecharge,
			code: response.CodeBadRequest,
			msg:  "钱包充值不支持此支付渠道",
		},
		{
			name: "unknown error",
			err:  errors.New("boom"),
			code: response.CodeInternal,
			msg:  "创建支付失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := newResponseTestContext()

			respondPaymentCreateError(c, tt.err)

			assertErrorResponse(t, recorder, tt.code, tt.msg)
		})
	}
}

func TestRespondPaymentCaptureError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		err  error
		code int
		msg  string
	}{
		{
			name: "amount mismatch",
			err:  ErrPaymentAmountMismatch,
			code: response.CodeBadRequest,
			msg:  "支付金额不匹配",
		},
		{
			name: "unknown error",
			err:  errors.New("boom"),
			code: response.CodeInternal,
			msg:  "支付回调处理失败",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, recorder := newResponseTestContext()

			respondPaymentCaptureError(c, tt.err)

			assertErrorResponse(t, recorder, tt.code, tt.msg)
		})
	}
}

func newResponseTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c, recorder
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, wantCode int, wantMsg string) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var body response.Response
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.StatusCode != wantCode {
		t.Fatalf("status_code = %d, want %d; body=%s", body.StatusCode, wantCode, recorder.Body.String())
	}
	if body.Msg != wantMsg {
		t.Fatalf("msg = %q, want %q; body=%s", body.Msg, wantMsg, recorder.Body.String())
	}
}
