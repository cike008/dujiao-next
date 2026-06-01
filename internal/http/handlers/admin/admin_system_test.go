package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dujiao-next/internal/provider"
	"github.com/dujiao-next/internal/version"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSystemVersion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{Container: &provider.Container{}}
	r := gin.New()
	r.GET("/system/version", h.GetSystemVersion)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/system/version", nil))

	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"version":"`+version.Version+`"`)
	assert.Contains(t, body, `"current_version":"`+version.Version+`"`)
	assert.Contains(t, body, `"api_version":"`+version.Version+`"`)
}
