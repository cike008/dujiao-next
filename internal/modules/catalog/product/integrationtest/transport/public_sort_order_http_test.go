package integrationtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"
	producthttp "github.com/dujiao-next/internal/modules/catalog/product/transport/http"
	"github.com/gin-gonic/gin"
)

func TestPublicProductHTTPIncludesSortOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := producthttp.NewPublicHandler(
		staticPublicProductQueries{product: productdomain.Product{ID: 1, Slug: "sorted-product", SortOrder: 42}},
		nil, nil, nil, nil, nil, emptyRelatedPostReader{},
	)
	router := gin.New()
	producthttp.RegisterPublicRoutes(router, handler)

	for _, path := range []string{"/products", "/products/sorted-product"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d body=%s", recorder.Code, recorder.Body.String())
			}

			var envelope struct {
				Data json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			var products []map[string]json.RawMessage
			if path == "/products" {
				if err := json.Unmarshal(envelope.Data, &products); err != nil {
					t.Fatalf("decode product list: %v", err)
				}
			} else {
				var product map[string]json.RawMessage
				if err := json.Unmarshal(envelope.Data, &product); err != nil {
					t.Fatalf("decode product detail: %v", err)
				}
				products = []map[string]json.RawMessage{product}
			}
			if len(products) != 1 || string(products[0]["sort_order"]) != "42" {
				t.Fatalf("expected sort_order 42 in response, got %s", envelope.Data)
			}
		})
	}
}
