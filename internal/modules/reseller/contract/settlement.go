package contract

import resellerdomain "github.com/dujiao-next/internal/modules/reseller/domain"

type SettlementSettingStore interface {
	GetSettlementSetting() (*resellerdomain.SettlementSetting, error)
	SaveSettlementSetting(*resellerdomain.SettlementSetting) error
}
