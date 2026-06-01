package admin

import (
	"context"
	"errors"
	"time"

	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"
	"github.com/dujiao-next/internal/version"

	"github.com/gin-gonic/gin"
)

// GetSystemVersion 返回当前 API 版本。
// GET /api/v1/admin/system/version
func (h *Handler) GetSystemVersion(c *gin.Context) {
	current := version.Version
	response.Success(c, gin.H{
		"version":         current,
		"current_version": current,
		"api_version":     current,
		"source":          "build",
	})
}

// CheckSystemUpdate 通过 GitHub Releases API 检测是否有新版本发布
// GET /api/v1/admin/system/version/check
func (h *Handler) CheckSystemUpdate(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 12*time.Second)
	defer cancel()

	result, err := version.CheckLatestRelease(ctx)
	if err != nil {
		if errors.Is(err, version.ErrRateLimited) {
			shared.RespondError(c, response.CodeTooManyRequests, "error.update_check_rate_limited", err)
			return
		}
		shared.RespondError(c, response.CodeInternal, "error.update_check_failed", err)
		return
	}

	response.Success(c, result)
}
