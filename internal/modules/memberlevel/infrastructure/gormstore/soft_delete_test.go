package gormstore

import (
	"fmt"
	"os"
	"testing"
	"time"

	memberlevelcontract "github.com/dujiao-next/internal/modules/memberlevel/contract"
	memberleveldomain "github.com/dujiao-next/internal/modules/memberlevel/domain"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func newMemberLevelStoreTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:memberlevel_store_%d?mode=memory&cache=shared", time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if pgDSN := os.Getenv("MEMBERLEVEL_TEST_POSTGRES_DSN"); pgDSN != "" {
		db, err = gorm.Open(postgres.Open(pgDSN), &gorm.Config{})
		if err == nil {
			schema := fmt.Sprintf("memberlevel_test_%d", time.Now().UnixNano())
			connection, sqlErr := db.DB()
			if sqlErr != nil {
				t.Fatal(sqlErr)
			}
			connection.SetMaxOpenConns(1)
			if err := db.Exec("CREATE SCHEMA " + schema).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.Exec("SET search_path TO " + schema).Error; err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE"); connection.Close() })
		}
	}
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&memberleveldomain.MemberLevel{}, &memberleveldomain.MemberLevelPrice{}); err != nil {
		t.Fatalf("auto migrate: %v", err)
	}
	return db
}

func createStoreTestLevel(t *testing.T, db *gorm.DB) memberleveldomain.MemberLevel {
	t.Helper()

	level := memberleveldomain.MemberLevel{
		NameJSON:          jsonmap.JSON{"zh-CN": "VIP"},
		Slug:              "vip",
		DiscountRate:      money.FromDecimal(decimal.NewFromInt(90)),
		RechargeThreshold: money.FromDecimal(decimal.NewFromInt(100)),
		SpendThreshold:    money.FromDecimal(decimal.NewFromInt(100)),
		IsDefault:         true,
		SortOrder:         10,
		IsActive:          true,
	}
	if err := db.Create(&level).Error; err != nil {
		t.Fatalf("create level: %v", err)
	}
	return level
}

func TestLevelStoreSoftDeleteHidesLevelFromEveryReadPath(t *testing.T) {
	db := newMemberLevelStoreTestDB(t)
	level := createStoreTestLevel(t, db)
	store := NewLevelStore(db)

	if err := store.Delete(level.ID); err != nil {
		t.Fatalf("delete level: %v", err)
	}

	if got, err := store.GetByID(level.ID); err != nil || got != nil {
		t.Fatalf("GetByID after delete = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := store.GetBySlug(level.Slug); err != nil || got != nil {
		t.Fatalf("GetBySlug after delete = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := store.GetDefault(); err != nil || got != nil {
		t.Fatalf("GetDefault after delete = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := store.GetActiveBySortOrder(level.SortOrder, 0); err != nil || got != nil {
		t.Fatalf("GetActiveBySortOrder after delete = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := store.ListAllActive(); err != nil || len(got) != 0 {
		t.Fatalf("ListAllActive after delete = (%v, %v), want empty", got, err)
	}
	if got, total, err := store.List(memberlevelcontract.ListFilter{}); err != nil || total != 0 || len(got) != 0 {
		t.Fatalf("List after delete = (%v, %d, %v), want empty", got, total, err)
	}

	var raw memberleveldomain.MemberLevel
	if err := db.First(&raw, level.ID).Error; err != nil {
		t.Fatalf("load raw deleted level: %v", err)
	}
	if raw.DeletedAt == nil {
		t.Fatal("deleted level must retain a non-nil deleted_at marker")
	}
}

func TestPriceStoreResavesDeletedPriceWithoutRevivingOtherPrices(t *testing.T) {
	for _, sku := range []uint{0, 200} {
		t.Run(fmt.Sprint(sku), func(t *testing.T) {
			db := newMemberLevelStoreTestDB(t)
			level := createStoreTestLevel(t, db)
			store := NewPriceStore(db)
			price := memberleveldomain.MemberLevelPrice{MemberLevelID: level.ID, ProductID: 100, SKUID: sku, PriceAmount: money.FromDecimal(decimal.NewFromInt(8))}
			other := price
			other.ProductID = 101
			if err := store.BatchUpsert([]memberleveldomain.MemberLevelPrice{price, other}); err != nil {
				t.Fatal(err)
			}
			original, err := store.GetByLevelAndProductAndSKU(level.ID, 100, sku)
			if err != nil || original == nil {
				t.Fatalf("missing original: %v", err)
			}
			if err := store.Delete(original.ID); err != nil {
				t.Fatal(err)
			}
			if err := store.DeleteByProduct(101); err != nil {
				t.Fatal(err)
			}
			price.PriceAmount = money.FromDecimal(decimal.NewFromInt(6))
			for i := 0; i < 2; i++ {
				if err := store.BatchUpsert([]memberleveldomain.MemberLevelPrice{price}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := store.GetByLevelAndProductAndSKU(level.ID, 100, sku)
			if err != nil || got == nil || got.ID != original.ID || got.DeletedAt != nil || got.PriceAmount.String() != price.PriceAmount.String() {
				t.Fatalf("unexpected restored price: %v, %v", got, err)
			}
			if got, err := store.ListByProduct(101); err != nil || len(got) != 0 {
				t.Fatalf("unrelated price revived: %v, %v", got, err)
			}
			var count int64
			db.Model(&memberleveldomain.MemberLevelPrice{}).Count(&count)
			if count != 2 {
				t.Fatalf("unexpected row count: %d", count)
			}
		})
	}
}

func TestPriceStoreBatchFailureRollsBackRestoredPrice(t *testing.T) {
	db := newMemberLevelStoreTestDB(t)
	store := NewPriceStore(db)
	price := memberleveldomain.MemberLevelPrice{MemberLevelID: 1, ProductID: 100, PriceAmount: money.FromDecimal(decimal.NewFromInt(8))}
	if err := store.BatchUpsert([]memberleveldomain.MemberLevelPrice{price}); err != nil {
		t.Fatal(err)
	}
	original, err := store.GetByLevelAndProductAndSKU(1, 100, 0)
	if err != nil || original == nil {
		t.Fatalf("missing price: %v", err)
	}
	if err := store.Delete(original.ID); err != nil {
		t.Fatal(err)
	}
	// The second write violates a database constraint after the first restores the price.
	if err := db.Exec("CREATE UNIQUE INDEX test_price_amount_unique ON member_level_prices (price_amount)").Error; err != nil {
		t.Fatal(err)
	}
	price.PriceAmount = money.FromDecimal(decimal.NewFromInt(6))
	other := price
	other.ProductID = 101
	if err := store.BatchUpsert([]memberleveldomain.MemberLevelPrice{price, other}); err == nil {
		t.Fatal("expected constraint failure")
	}
	var raw memberleveldomain.MemberLevelPrice
	if err := db.First(&raw, original.ID).Error; err != nil {
		t.Fatal(err)
	}
	if raw.DeletedAt == nil || raw.PriceAmount.String() != "8.00" {
		t.Fatalf("partial batch persisted: %+v", raw)
	}
}

func TestPriceStoreSoftDeleteHidesPriceFromEveryReadPath(t *testing.T) {
	db := newMemberLevelStoreTestDB(t)
	level := createStoreTestLevel(t, db)
	price := memberleveldomain.MemberLevelPrice{
		MemberLevelID: level.ID,
		ProductID:     100,
		SKUID:         200,
		PriceAmount:   money.FromDecimal(decimal.NewFromInt(8)),
	}
	if err := db.Create(&price).Error; err != nil {
		t.Fatalf("create price: %v", err)
	}
	store := NewPriceStore(db)

	if err := store.DeleteByProduct(price.ProductID); err != nil {
		t.Fatalf("delete prices by product: %v", err)
	}

	if got, err := store.GetByID(price.ID); err != nil || got != nil {
		t.Fatalf("GetByID after delete = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := store.GetByLevelAndProductAndSKU(level.ID, price.ProductID, price.SKUID); err != nil || got != nil {
		t.Fatalf("GetByLevelAndProductAndSKU after delete = (%v, %v), want (nil, nil)", got, err)
	}
	if got, err := store.ListByProduct(price.ProductID); err != nil || len(got) != 0 {
		t.Fatalf("ListByProduct after delete = (%v, %v), want empty", got, err)
	}
	if got, err := store.ListByLevelAndProducts(level.ID, []uint{price.ProductID}); err != nil || len(got) != 0 {
		t.Fatalf("ListByLevelAndProducts after delete = (%v, %v), want empty", got, err)
	}

	var raw memberleveldomain.MemberLevelPrice
	if err := db.First(&raw, price.ID).Error; err != nil {
		t.Fatalf("load raw deleted price: %v", err)
	}
	if raw.DeletedAt == nil {
		t.Fatal("deleted price must retain a non-nil deleted_at marker")
	}
}
