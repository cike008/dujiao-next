package admin

import (
	"github.com/dujiao-next/internal/cache"
	"github.com/dujiao-next/internal/http/handlers/shared"
	"github.com/dujiao-next/internal/http/response"

	"github.com/gin-gonic/gin"
)

// GetContactItems 获取扩展联系方式配置。
func (h *Handler) GetContactItems(c *gin.Context) {
	items, err := h.SettingService.GetSiteContactItems()
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.settings_fetch_failed", err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

// UpdateContactItems 更新扩展联系方式配置。
func (h *Handler) UpdateContactItems(c *gin.Context) {
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		shared.RespondBindError(c, err)
		return
	}

	rawItems, ok := req["items"]
	if !ok {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}
	items, ok := rawItems.([]interface{})
	if !ok {
		shared.RespondError(c, response.CodeBadRequest, "error.bad_request", nil)
		return
	}

	updated, err := h.SettingService.UpdateSiteContactItems(items)
	if err != nil {
		shared.RespondError(c, response.CodeInternal, "error.settings_save_failed", err)
		return
	}
	_ = cache.Del(c.Request.Context(), publicConfigCacheKey)
	response.Success(c, gin.H{"items": updated})
}
