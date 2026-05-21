package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dujiao-next/internal/models"
)

func TestProductRespIncludesSortOrder(t *testing.T) {
	resp := ProductResp{
		ID:        1,
		Slug:      "netflix",
		SortOrder: 99,
		SKUs: []SKUResp{
			{ID: 10, SKUCode: "default", SortOrder: 20},
		},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal product resp: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"sort_order":99`) {
		t.Fatalf("product sort_order should appear, got %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"sort_order":20`) {
		t.Fatalf("sku sort_order should appear, got %s", jsonStr)
	}
}

func TestCategoryRespOmitsSensitiveFields(t *testing.T) {
	cat := &models.Category{
		ID:        1,
		ParentID:  0,
		Slug:      "games",
		NameJSON:  models.JSON{"zh-CN": "游戏"},
		Icon:      "/icons/game.png",
		SortOrder: 10,
	}

	resp := NewCategoryResp(cat)
	data, _ := json.Marshal(resp)
	jsonStr := string(data)

	if strings.Contains(jsonStr, `"created_at"`) {
		t.Error("created_at should not appear")
	}
	if !strings.Contains(jsonStr, `"slug"`) {
		t.Error("slug should appear")
	}
	if resp.Name["zh-CN"] != "游戏" {
		t.Error("name should be preserved")
	}
}

func TestCategoryRespListEmpty(t *testing.T) {
	result := NewCategoryRespList(nil)
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d", len(result))
	}
}
