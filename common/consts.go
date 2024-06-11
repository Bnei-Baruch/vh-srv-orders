package common

const (
	ServiceName = "vh-srv-orders"

	CtxEventBuilder = "EVENT_BUILDER"
	CtxRequestID    = "REQUEST_ID"
	CtxLogger       = "LOGGER"

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

	OrderStatusCancelled = "cancelled"
	//OrderStatusCancelledFailed = "cancelledFailed"
	OrderStatusNoSuccess = "nosuccess"
	OrderStatusPaid      = "paid"
	OrderStatusPending   = "pending"
	OrderStatusRefunded  = "refunded"

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
	PaymentStatusPending   = "pending"
	PaymentStatusRefunded  = "refunded"
	PaymentStatusSuccess   = "success"

	OfflinePaymentMethodRobokasa       = "robokasa"
	OfflinePaymentPropertiesRobokasaID = "robokasa_id"
	GetNewTokenEndpoint                = "https://checkout.kbb1.com/emv/new_token"
)

// This gets set at build time via `-ldflags "-X ..."`
var GitSHA string = "local"
