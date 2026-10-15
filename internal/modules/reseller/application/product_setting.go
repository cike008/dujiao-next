package application

import (
	"errors"
	"strconv"
	"strings"

	resellercontract "github.com/dujiao-next/internal/modules/reseller/contract"

	resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

	productcontract "github.com/dujiao-next/internal/modules/catalog/product/contract"
	productdomain "github.com/dujiao-next/internal/modules/catalog/product/domain"

	"github.com/dujiao-next/internal/shared/money"
	"github.com/shopspring/decimal"
)

// ProductSettingService 分销商品改价/上架配置用例。
type ProductSettingService struct {
	store       resellercontract.ProductSettingStore
	productRepo productcontract.Repository
	supply      resellercontract.SupplyPriceResolver
}

func (s *ProductSettingService) SetSupplyPriceResolver(resolver resellercontract.SupplyPriceResolver) {
	s.supply = resolver
}

func (s *ProductSettingService) loadSupplyPrices(profile *resellerdomain.Profile, productIDs []uint) (resellercontract.SupplyPrices, error) {
	if s.supply == nil {
		return nil, nil
	}
	return s.supply.ResolveSupplyPrices(profile.UserID, productIDs)
}

func NewProductSettingService(store resellercontract.ProductSettingStore, productRepo productcontract.Repository) *ProductSettingService {
	return &ProductSettingService{store: store, productRepo: productRepo}
}

type ProductSettingInput struct {
	SKUID             uint
	IsListed          bool
	PricingMode       string
	MarkupPercent     decimal.Decimal
	FixedMarkupAmount decimal.Decimal
	FixedPriceAmount  decimal.Decimal
	SortOrder         int
}

type ProductSettingSaveInput struct {
	Settings []ProductSettingInput
}

type ProductSettingUserListInput struct {
	Page       int
	PageSize   int
	Keyword    string
	CategoryID uint
	Configured string
	Listed     string
}

type ProductSettingAdminListInput struct {
	Page        int
	PageSize    int
	ResellerID  uint
	UserID      uint
	ProductID   uint
	Keyword     string
	PricingMode string
	Configured  string
	Listed      string
}

type ProductSettingDetail struct {
	SupplyPrices     resellercontract.SupplyPrices
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

type ProductSettingListRow struct {
	SupplyPrices     resellercontract.SupplyPrices
	Profile          *resellerdomain.Profile
	Product          productdomain.Product
	Settings         []resellerdomain.ProductSetting
	EffectiveBySKUID map[uint]decimal.Decimal
	RuleBySKUID      map[uint]string
}

// ProductSettingPreviewItem 表示某个商品级（SKUID=0）或 SKU 级规则在「拟用配置」下的预览结果。
// 复用与保存/下单完全一致的 ResolveUnitAmount + ValidateUnitAmount，确保预览价与实际成交价零分歧。
type ProductSettingPreviewItem struct {
	SKUID          uint
	IsListed       bool
	BasePrice      decimal.Decimal
	EffectivePrice decimal.Decimal
	Valid          bool
	ErrorCode      string
}

func (s *ProductSettingService) ListUserProductSettings(userID uint, input ProductSettingUserListInput) ([]ProductSettingListRow, int64, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, 0, err
	}
	rows, total, err := s.store.ListProductsWithSettings(resellercontract.ProductSettingListFilter{
		Page:       input.Page,
		PageSize:   input.PageSize,
		ResellerID: profile.ID,
		CategoryID: input.CategoryID,
		Keyword:    input.Keyword,
		Configured: input.Configured,
		Listed:     input.Listed,
		OnlyActive: true,
	})
	if err != nil {
		return nil, 0, err
	}
	decorated, err := s.decorateRows(profile, rows)
	return decorated, total, err
}

func (s *ProductSettingService) GetUserProductSetting(userID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewUserProductSettings 在不落库的前提下，按用户拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewUserProductSettings(userID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input)
}

func (s *ProductSettingService) SaveUserProductSettings(userID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetUserProductSetting(userID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByUser(userID)
	if err != nil {
		return err
	}
	return s.store.DeleteSetting(profile.ID, productID, skuID)
}

func (s *ProductSettingService) ListAdminSettings(input ProductSettingAdminListInput) ([]resellerdomain.ProductSetting, int64, error) {
	return s.store.ListAdminSettings(resellercontract.ProductSettingAdminListFilter(input))
}

func (s *ProductSettingService) SummarizeAdminSettings(resellerID uint) (resellercontract.ProductSettingSummary, error) {
	if s == nil || s.store == nil || resellerID == 0 {
		return resellercontract.ProductSettingSummary{}, nil
	}
	return s.store.SummarizeByResellerID(resellerID)
}

func (s *ProductSettingService) GetAdminProductSetting(resellerID, productID uint) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

// PreviewAdminProductSettings 在不落库的前提下，按管理员拟用的定价规则计算各 SKU 的预计生效价与校验结果。
func (s *ProductSettingService) PreviewAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	return s.previewSettings(profile, productID, input)
}

func (s *ProductSettingService) SaveAdminProductSettings(resellerID, productID uint, input ProductSettingSaveInput) (*ProductSettingDetail, error) {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if err := s.saveSettings(profile, productID, input); err != nil {
		return nil, err
	}
	return s.getDetail(profile, productID)
}

func (s *ProductSettingService) ResetAdminProductSetting(resellerID, productID, skuID uint) error {
	profile, err := s.requireActiveProfileByID(resellerID)
	if err != nil {
		return err
	}
	return s.store.DeleteSetting(profile.ID, productID, skuID)
}

func (s *ProductSettingService) requireActiveProfileByUser(userID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || userID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByUserID(userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, resellercontract.ErrNotOpened
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) requireActiveProfileByID(resellerID uint) (*resellerdomain.Profile, error) {
	if s == nil || s.store == nil || s.productRepo == nil || resellerID == 0 {
		return nil, productcontract.ErrNotFound
	}
	profile, err := s.store.GetProfileByID(resellerID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, productcontract.ErrNotFound
	}
	if profile.Status != resellerdomain.ProfileStatusActive {
		return nil, resellercontract.ErrProfileInactive
	}
	return profile, nil
}

func (s *ProductSettingService) getDetail(profile *resellerdomain.Profile, productID uint) (*ProductSettingDetail, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	prices, err := s.loadSupplyPrices(profile, []uint{productID})
	if err != nil {
		return nil, err
	}
	effective, rules, err := computeProductEffectivePrices(profile, row.Product, row.Settings, prices)
	if err != nil {
		return nil, err
	}
	return &ProductSettingDetail{SupplyPrices: prices, Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules}, nil
}

func (s *ProductSettingService) decorateRows(profile *resellerdomain.Profile, rows []resellercontract.ProductSettingProductRow) ([]ProductSettingListRow, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Product.ID)
	}
	prices, err := s.loadSupplyPrices(profile, ids)
	if err != nil {
		return nil, err
	}
	out := make([]ProductSettingListRow, 0, len(rows))
	for _, row := range rows {
		effective, rules, err := computeProductEffectivePrices(profile, row.Product, row.Settings, prices)
		if err != nil {
			return nil, err
		}
		out = append(out, ProductSettingListRow{SupplyPrices: prices, Profile: profile, Product: row.Product, Settings: row.Settings, EffectiveBySKUID: effective, RuleBySKUID: rules})
	}
	return out, nil
}

func (s *ProductSettingService) saveSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput) error {
	if profile == nil || productID == 0 {
		return productcontract.ErrNotFound
	}
	product, err := s.productRepo.GetAdminByID(strconv.FormatUint(uint64(productID), 10))
	if err != nil {
		return err
	}
	if product == nil {
		return productcontract.ErrNotFound
	}
	normalizedSettings := make([]resellerdomain.ProductSetting, 0, len(input.Settings))
	prices, err := s.loadSupplyPrices(profile, []uint{productID})
	if err != nil {
		return err
	}
	for _, item := range input.Settings {
		normalized, err := normalizeProductSettingInput(profile, product, item, prices)
		if err != nil {
			return err
		}
		normalized.ResellerID = profile.ID
		normalized.ProductID = product.ID
		normalizedSettings = append(normalizedSettings, normalized)
	}
	return s.store.WithinProductSettingTransaction(func(store resellercontract.ProductSettingStore) error {
		for _, setting := range normalizedSettings {
			if _, err := store.UpsertSetting(setting); err != nil {
				return err
			}
		}
		return nil
	})
}

func normalizeProductSettingInput(profile *resellerdomain.Profile, product *productdomain.Product, input ProductSettingInput, supply ...resellercontract.SupplyPrices) (resellerdomain.ProductSetting, error) {
	var prices resellercontract.SupplyPrices
	if len(supply) > 0 {
		prices = supply[0]
	}
	if product == nil {
		return resellerdomain.ProductSetting{}, productcontract.ErrNotFound
	}
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeMarkupPercent, resellerdomain.PricingModeFixedMarkup, resellerdomain.PricingModeFixedPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	setting := resellerdomain.ProductSetting{
		SKUID:             input.SKUID,
		IsListed:          input.IsListed,
		PricingMode:       mode,
		MarkupPercent:     money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount: money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:  money.FromDecimal(input.FixedPriceAmount.Round(2)),
		SortOrder:         input.SortOrder,
	}
	if !setting.IsListed {
		return setting, nil
	}
	if input.SKUID > 0 {
		sku := findProductSKU(product.SKUs, input.SKUID)
		if sku == nil || !sku.IsActive {
			return resellerdomain.ProductSetting{}, productcontract.ErrProductSKUInvalid
		}
		base := prices.Unit(product.ID, sku.ID, sku.PriceAmount.Decimal)
		price, _, err := ResolveUnitAmount(profile, nil, &setting, base, sku.PriceAmount.Decimal)
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateUnitAmount(profile, sku, base, price); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		return setting, nil
	}
	if len(product.SKUs) == 0 {
		basePrice := prices.Unit(product.ID, 0, product.PriceAmount.Decimal)
		price, _, err := ResolveUnitAmount(profile, &setting, nil, basePrice, product.PriceAmount.Decimal)
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateUnitAmount(profile, nil, basePrice, price); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		costPrice := product.CostPriceAmount.Decimal.Round(2)
		if costPrice.GreaterThan(decimal.Zero) && price.LessThan(costPrice) {
			return resellerdomain.ProductSetting{}, resellercontract.ErrPriceBelowBase
		}
		return setting, nil
	}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		base := prices.Unit(product.ID, sku.ID, sku.PriceAmount.Decimal)
		price, _, err := ResolveUnitAmount(profile, &setting, nil, base, sku.PriceAmount.Decimal)
		if err != nil {
			return resellerdomain.ProductSetting{}, err
		}
		if err := ValidateUnitAmount(profile, sku, base, price); err != nil {
			return resellerdomain.ProductSetting{}, err
		}
	}
	return setting, nil
}

func (s *ProductSettingService) previewSettings(profile *resellerdomain.Profile, productID uint, input ProductSettingSaveInput) ([]ProductSettingPreviewItem, error) {
	if profile == nil || productID == 0 {
		return nil, productcontract.ErrNotFound
	}
	row, err := s.store.GetProductWithSettings(profile.ID, productID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, productcontract.ErrNotFound
	}
	product := row.Product
	prices, err := s.loadSupplyPrices(profile, []uint{productID})
	if err != nil {
		return nil, err
	}

	var productSetting *resellerdomain.ProductSetting
	skuSettings := map[uint]*resellerdomain.ProductSetting{}
	for _, saved := range row.Settings {
		setting := saved
		if setting.SKUID == 0 {
			productSetting = &setting
		} else {
			skuSettings[setting.SKUID] = &setting
		}
	}
	for _, item := range input.Settings {
		if item.SKUID > 0 {
			sku := findProductSKU(product.SKUs, item.SKUID)
			if sku == nil || !sku.IsActive {
				return nil, productcontract.ErrProductSKUInvalid
			}
		}
		setting, err := buildPreviewSetting(item)
		if err != nil {
			return nil, err
		}
		setting.ResellerID = profile.ID
		setting.ProductID = product.ID
		if item.SKUID == 0 {
			ps := setting
			productSetting = &ps
		} else {
			ss := setting
			skuSettings[item.SKUID] = &ss
		}
	}

	items := make([]ProductSettingPreviewItem, 0, len(product.SKUs)+1)

	productBase := prices.Unit(product.ID, 0, product.PriceAmount.Decimal)
	productItem := ProductSettingPreviewItem{
		SKUID:     0,
		IsListed:  productSetting == nil || productSetting.IsListed,
		BasePrice: productBase,
		Valid:     true,
	}
	if productSetting == nil || productSetting.IsListed {
		price, _, _ := ResolveUnitAmount(profile, productSetting, nil, productBase, product.PriceAmount.Decimal)
		productItem.EffectivePrice = price.Round(2)
		if len(product.SKUs) == 0 {
			perr := ValidateUnitAmount(profile, nil, productBase, price)
			if perr == nil {
				cost := product.CostPriceAmount.Decimal.Round(2)
				if cost.GreaterThan(decimal.Zero) && price.LessThan(cost) {
					perr = resellercontract.ErrPriceBelowBase
				}
			}
			productItem.Valid = perr == nil
			productItem.ErrorCode = previewErrorCode(perr)
		} else {
			productItem.Valid, productItem.ErrorCode = previewValidateProductRuleAcrossSKUs(profile, product, productSetting, prices)
		}
	}
	items = append(items, productItem)

	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		skuSetting := skuSettings[sku.ID]
		base := prices.Unit(product.ID, sku.ID, sku.PriceAmount.Decimal)
		item := ProductSettingPreviewItem{SKUID: sku.ID, IsListed: true, BasePrice: base, Valid: true}
		if (productSetting != nil && !productSetting.IsListed) || (skuSetting != nil && !skuSetting.IsListed) {
			item.IsListed = false
			items = append(items, item)
			continue
		}
		price, _, perr := ResolveUnitAmount(profile, productSetting, skuSetting, base, sku.PriceAmount.Decimal)
		if perr == nil {
			perr = ValidateUnitAmount(profile, sku, base, price)
		}
		item.EffectivePrice = price.Round(2)
		item.Valid = perr == nil
		item.ErrorCode = previewErrorCode(perr)
		items = append(items, item)
	}
	return items, nil
}

func buildPreviewSetting(input ProductSettingInput) (resellerdomain.ProductSetting, error) {
	mode := strings.TrimSpace(input.PricingMode)
	if mode == "" {
		mode = resellerdomain.PricingModeInherit
	}
	switch mode {
	case resellerdomain.PricingModeInherit, resellerdomain.PricingModeMarkupPercent, resellerdomain.PricingModeFixedMarkup, resellerdomain.PricingModeFixedPrice:
	default:
		return resellerdomain.ProductSetting{}, resellercontract.ErrPricingModeInvalid
	}
	return resellerdomain.ProductSetting{
		SKUID:             input.SKUID,
		IsListed:          input.IsListed,
		PricingMode:       mode,
		MarkupPercent:     money.FromDecimal(input.MarkupPercent.Round(2)),
		FixedMarkupAmount: money.FromDecimal(input.FixedMarkupAmount.Round(2)),
		FixedPriceAmount:  money.FromDecimal(input.FixedPriceAmount.Round(2)),
		SortOrder:         input.SortOrder,
	}, nil
}

func previewValidateProductRuleAcrossSKUs(profile *resellerdomain.Profile, product productdomain.Product, productSetting *resellerdomain.ProductSetting, prices resellercontract.SupplyPrices) (bool, string) {
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		base := prices.Unit(product.ID, sku.ID, sku.PriceAmount.Decimal)
		price, _, err := ResolveUnitAmount(profile, productSetting, nil, base, sku.PriceAmount.Decimal)
		if err == nil {
			err = ValidateUnitAmount(profile, sku, base, price)
		}
		if err != nil {
			return false, previewErrorCode(err)
		}
	}
	return true, ""
}

func previewErrorCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, resellercontract.ErrMarkupExceeded):
		return "markup_exceeded"
	default:
		return "price_invalid"
	}
}

func computeProductEffectivePrices(profile *resellerdomain.Profile, product productdomain.Product, settings []resellerdomain.ProductSetting, supply ...resellercontract.SupplyPrices) (map[uint]decimal.Decimal, map[uint]string, error) {
	var prices resellercontract.SupplyPrices
	if len(supply) > 0 {
		prices = supply[0]
	}
	effective := map[uint]decimal.Decimal{}
	rules := map[uint]string{}
	byProduct, bySKU := indexProductSettings(settings)
	productSetting := byProduct[product.ID]
	if productSetting != nil && productSetting.IsListed {
		price, rule, err := ResolveUnitAmount(profile, productSetting, nil, prices.Unit(product.ID, 0, product.PriceAmount.Decimal), product.PriceAmount.Decimal)
		if err != nil {
			return effective, rules, err
		}
		effective[0] = price.Round(2)
		rules[0] = rule.Source
	}
	for i := range product.SKUs {
		sku := &product.SKUs[i]
		if !sku.IsActive {
			continue
		}
		skuSetting := bySKU[productSettingKey{productID: product.ID, skuID: sku.ID}]
		if productSetting != nil && !productSetting.IsListed {
			continue
		}
		if skuSetting != nil && !skuSetting.IsListed {
			continue
		}
		price, rule, err := ResolveUnitAmount(profile, productSetting, skuSetting, prices.Unit(product.ID, sku.ID, sku.PriceAmount.Decimal), sku.PriceAmount.Decimal)
		if err != nil {
			return effective, rules, err
		}
		effective[sku.ID] = price.Round(2)
		rules[sku.ID] = rule.Source
	}
	return effective, rules, nil
}

type productSettingKey struct {
	productID uint
	skuID     uint
}

func indexProductSettings(settings []resellerdomain.ProductSetting) (map[uint]*resellerdomain.ProductSetting, map[productSettingKey]*resellerdomain.ProductSetting) {
	byProduct := make(map[uint]*resellerdomain.ProductSetting)
	bySKU := make(map[productSettingKey]*resellerdomain.ProductSetting)
	for i := range settings {
		setting := settings[i]
		row := setting
		if setting.SKUID == 0 {
			byProduct[setting.ProductID] = &row
			continue
		}
		bySKU[productSettingKey{productID: setting.ProductID, skuID: setting.SKUID}] = &row
	}
	return byProduct, bySKU
}

func findProductSKU(items []productdomain.ProductSKU, skuID uint) *productdomain.ProductSKU {
	for i := range items {
		if items[i].ID == skuID {
			return &items[i]
		}
	}
	return nil
}
