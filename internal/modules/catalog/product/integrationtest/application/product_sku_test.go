package integrationtest

import (
	"strconv"
	"testing"

	cardsecretdomain "github.com/dujiao-next/internal/modules/cardsecret/domain"
	productwrite "github.com/dujiao-next/internal/modules/catalog/product/application/write"
	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	categorydomain "github.com/dujiao-next/internal/modules/catalog/category/domain"

	"github.com/dujiao-next/internal/constants"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

func TestProductServiceUpdateRejectsDisablingAutoSKUWithCardSecretStock(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	category := categorydomain.Category{
		Slug:     "auto-card-secret-category",
		NameJSON: jsonmap.JSON{"zh-CN": "auto-card-secret-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "auto-card-secret-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "auto-card-secret-product"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(10)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	stockSKU := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "SKU-STOCK",
		SpecValuesJSON: jsonmap.JSON{"zh-CN": "有库存"},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:       true,
		SortOrder:      2,
	}
	spareSKU := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "SKU-SPARE",
		SpecValuesJSON: jsonmap.JSON{"zh-CN": "无库存"},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:       true,
		SortOrder:      1,
	}
	if err := db.Create(&stockSKU).Error; err != nil {
		t.Fatalf("create stock sku failed: %v", err)
	}
	if err := db.Create(&spareSKU).Error; err != nil {
		t.Fatalf("create spare sku failed: %v", err)
	}

	insertCardSecrets(t, db, product.ID, stockSKU.ID, cardsecretdomain.StatusAvailable, 1)

	_, err := svc.Write.Update(strconv.FormatUint(uint64(product.ID), 10), productwrite.CreateProductInput{
		CategoryID:      category.ID,
		Slug:            product.Slug,
		TitleJSON:       map[string]interface{}{"zh-CN": "auto-card-secret-product"},
		PriceAmount:     decimal.NewFromInt(10),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		SKUs: []productwrite.ProductSKUInput{
			{
				ID:             stockSKU.ID,
				SKUCode:        stockSKU.SKUCode,
				SpecValuesJSON: map[string]interface{}{"zh-CN": "有库存"},
				PriceAmount:    decimal.NewFromInt(10),
				IsActive: func() *bool {
					value := false
					return &value
				}(),
				SortOrder: 2,
			},
			{
				ID:             spareSKU.ID,
				SKUCode:        spareSKU.SKUCode,
				SpecValuesJSON: map[string]interface{}{"zh-CN": "无库存"},
				PriceAmount:    decimal.NewFromInt(10),
				IsActive: func() *bool {
					value := true
					return &value
				}(),
				SortOrder: 1,
			},
		},
		IsActive: func() *bool {
			value := true
			return &value
		}(),
	})
	if err != productcontract.ErrProductSKUHasCardSecretStock {
		t.Fatalf("update product error want %v got %v", productcontract.ErrProductSKUHasCardSecretStock, err)
	}
}

func TestProductServiceUpdatePermanentlyRemovesOmittedSKU(t *testing.T) {
	svc, db := newProductServiceForTest(t)

	category := categorydomain.Category{
		Slug:     "sku-removal-category",
		NameJSON: jsonmap.JSON{"zh-CN": "sku-removal-category"},
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category failed: %v", err)
	}

	product := productdomain.Product{
		CategoryID:      category.ID,
		Slug:            "sku-removal-product",
		TitleJSON:       jsonmap.JSON{"zh-CN": "sku-removal-product"},
		PriceAmount:     money.FromDecimal(decimal.NewFromInt(10)),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		IsActive:        true,
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product failed: %v", err)
	}

	removedSKU := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "SKU-REMOVE",
		SpecValuesJSON: jsonmap.JSON{"zh-CN": "移除"},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(10)),
		IsActive:       true,
		SortOrder:      2,
	}
	keptSKU := productdomain.ProductSKU{
		ProductID:      product.ID,
		SKUCode:        "SKU-KEEP",
		SpecValuesJSON: jsonmap.JSON{"zh-CN": "保留"},
		PriceAmount:    money.FromDecimal(decimal.NewFromInt(20)),
		IsActive:       true,
		SortOrder:      1,
	}
	if err := db.Create(&removedSKU).Error; err != nil {
		t.Fatalf("create removed sku failed: %v", err)
	}
	if err := db.Create(&keptSKU).Error; err != nil {
		t.Fatalf("create kept sku failed: %v", err)
	}

	updated, err := svc.Write.Update(strconv.FormatUint(uint64(product.ID), 10), productwrite.CreateProductInput{
		CategoryID:      category.ID,
		Slug:            product.Slug,
		TitleJSON:       map[string]interface{}{"zh-CN": "sku-removal-product"},
		PriceAmount:     decimal.NewFromInt(20),
		PurchaseType:    constants.ProductPurchaseMember,
		FulfillmentType: constants.FulfillmentTypeAuto,
		SKUs: []productwrite.ProductSKUInput{
			{
				ID:             keptSKU.ID,
				SKUCode:        keptSKU.SKUCode,
				SpecValuesJSON: map[string]interface{}{"zh-CN": "保留"},
				PriceAmount:    decimal.NewFromInt(20),
				IsActive:       boolPointer(true),
				SortOrder:      1,
			},
		},
		IsActive: boolPointer(true),
	})
	if err != nil {
		t.Fatalf("update product failed: %v", err)
	}
	if len(updated.SKUs) != 1 || updated.SKUs[0].ID != keptSKU.ID {
		t.Fatalf("expected only kept sku after update, got %+v", updated.SKUs)
	}

	var removedCount int64
	if err := db.Unscoped().Model(&productdomain.ProductSKU{}).Where("id = ?", removedSKU.ID).Count(&removedCount).Error; err != nil {
		t.Fatalf("count removed sku failed: %v", err)
	}
	if removedCount != 0 {
		t.Fatalf("removed sku was restored by product save")
	}
}

func boolPointer(value bool) *bool {
	return &value
}
