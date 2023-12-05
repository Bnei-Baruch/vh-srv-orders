package common

const (
	CurrencyUSD = "USD"
	CurrencyEUR = "EUR"
	CurrencyNIS = "NIS"
	CurrencyRUR = "RUR"

	AccountTypePersonal = "personal"

	ProductTypeGlobalMembership = "globalmembership"
	ProductTypeDonation         = "donation"

	ProductSKU40033 = "40033" // miscellaneous
	ProductSKU40037 = "40037" // globalmembership

	OrderTypeRecurring = "recurring"
	OrderTypeRegular   = "regular"

	OrderStatusCancelled       = "cancelled"
	OrderStatusCancelledFailed = "cancelledFailed"
	OrderStatusNoSuccess       = "nosuccess"
	OrderStatusNulled          = "nulled"
	OrderStatusPaid            = "paid"
	OrderStatusPaused          = "paused"
	OrderStatusPending         = "pending"
	OrderStatusRemoved         = "removed"
	OrderStatusStopped         = "stopped"

	OrderLanguageEnglish = "EN"
	OrderLanguageHebrew  = "HE"
	OrderLanguageRussian = "RU"
	OrderLanguageSpanish = "ES"

	PaymentTypePelecard  = "pelecard"
	PaymentTypeManual    = "manual"
	PaymentTypeOffline   = "offline"
	PaymentTypeHelpHaver = "helphaver"

	PaymentStatusCancelled = "cancelled"
	PaymentStatusFailed    = "failed"
	PaymentStatusInvalid   = "invalid"
	PaymentStatusNoSuccess = "nosuccess"
	PaymentStatusPaid      = "paid"
	PaymentStatusPending   = "pending"
	PaymentStatusRefunded  = "refunded"
	PaymentStatusSuccess   = "success"

	OfflinePaymentMethodRobokasa       = "robokasa"
	OfflinePaymentPropertiesRobokasaID = "robokasa_id"
)
