package repo

import (
	"time"

	"github.com/volatiletech/null/v9"
)

// Order is defined by
type Order struct {
	ID        int       `json:"ID" gorm:"primary_key"`
	CreatedAt null.Time `json:"created_at"`
	UpdatedAt null.Time `json:"updated_at"`
	DeletedAt null.Time `json:"deleted_at" sql:"index"`

	Type         null.String `json:"type" gorm:"Column:Type;type:varchar(100)"`
	ProductType  null.String `json:"ProductType" gorm:"Column:ProductType;type:varchar(100)"`
	RecuringFreq null.Int    `json:"RecuringFreq,omitempty" gorm:"Column:RecuringFreq;type:int" sql:"DEFAULT:0"`

	AccountID    null.Int    `json:"AccountID" gorm:"Column:AccountID;"`
	Organization null.String `json:"Organization" gorm:"Column:Organization;type:varchar(10)"`

	Amount        null.Float64 `json:"Amount" gorm:"Column:Amount;type:varchar(85)"`
	Currency      null.String  `json:"Currency"  gorm:"Column:Currency;type:varchar(10)"`
	SKU           null.String  `json:"SKU"  gorm:"Column:SKU;type:varchar(30)"`
	Status        null.String  `json:"Status,omitempty" gorm:"Column:Status;type:varchar(85)"`
	OrderLanguage null.String  `json:"OrderLanguage,omitempty" gorm:"Column:OrderLanguage;type:varchar(10)"`
	PaymentDate   null.Time    `json:"PaymentDate" gorm:"Column:PaymentDate"`
	Note          null.String  `json:"Note" gorm:"Column:Note;type:varchar(200)"`
	Flag          null.String  `json:"Flag" gorm:"Column:Flag;type:varchar(200)"`
	Quantity      null.Int     `json:"Quantity"`
	AmountItem    null.Int     `json:"AmountItem"`
	StartingDate  null.Time    `json:"StartingDate"`
	CardDetailsId null.Int     `json:"card_details_id" gorm:"Column:card_details_id"`
	Payments      []Payment    `json:"Payments" gorm:"foreignkey:OrderID"`
}

// Payment is defined by
type Payment struct {
	ID        int       `json:"ID" gorm:"primary_key"`
	CreatedAt null.Time `json:"created_at"`
	UpdatedAt null.Time `json:"updated_at"`
	DeletedAt null.Time `json:"deleted_at" sql:"index"`

	Amount   null.Float64 `json:"Amount" gorm:"Column:Amount"`
	Currency null.String  `json:"Currency"`

	PaymentStatus null.String `json:"PaymentStatus" gorm:"Column:PaymentStatus"`
	PaymentType   null.String `json:"PaymentType" gorm:"Column:PaymentType;type:varchar(100)"`
	OrderID       null.Int    `json:"OrderID" gorm:"Column:OrderID"`

	ParamX          null.String `json:"additional_details_param_x" gorm:"Column:ParamX"`
	Ordkey          null.String `json:"user_key" gorm:"Column:Ordkey"`
	AuthNo          null.String `json:"authNo" gorm:"Column:AuthNo"`
	ConfirmationKey null.String `json:"confirmation_key" gorm:"ConfirmationKey"`
	Success         null.String `json:"success" gorm:"Success"`
	PelecardToken   null.String `json:"token" gorm:"PelecardToken"`
	TransactionID   null.String `json:"transaction_id" gorm:"Column:TransactionID"`
	ErrorMsg        null.String `json:"ErrorMsg" gorm:"Column:ErrorMsg"`

	CardHebrewName   null.String `json:"card_hebrew_name" gorm:"Column:CardHebrewName"`
	CCAbroadCard     null.String `json:"CCAbroadCard" gorm:"Column:CCAbroadCard"`
	CCBrand          null.String `json:"CCBrand" gorm:"Column:CCBrand"`
	CCCompanyClearer null.String `json:"CCCompanyClearer" gorm:"Column:CCCompanyClearer"`
	CCCompanyIssuer  null.String `json:"CCCompanyIssuer" gorm:"Column:CCCompanyIssuer"`
	CreditType       null.String `json:"credit_type" gorm:"CreditType"`

	CCExpDate null.String `json:"CCExpDate" gorm:"Column:CCExpDate"`
	CCNumber  null.String `json:"CCNumber" gorm:"Column:CCNumber"`

	DebitCode     null.String `json:"DebitCode" gorm:"Column:DebitCode"`
	DebitCurrency null.String `json:"DebitCurrency" gorm:"Column:DebitCurrency"`
	DebitTotal    null.String `json:"DebitTotal" gorm:"Column:DebitTotal"`
	DebitType     null.String `json:"DebitType" gorm:"Column:DebitType"`

	FirstPaymentTotal null.String `json:"FirstPaymentTotal" gorm:"Column:FirstPaymentTotal"`
	FixedPaymentTotal null.String `json:"FixedPaymentTotal" gorm:"Column:FixedPaymentTotal"`
	JParam            null.String `json:"j_param"`
	TotalPayments     null.String `json:"TotalPayments" gorm:"Column:TotalPayments"`

	TransactionInitTime   null.String `json:"TransactionInitTime" gorm:"Column:TransactionInitTime"`
	TransactionUpdateTime null.String `json:"TransactionUpdateTime" gorm:"Column:TransactionUpdateTime"`
	VoucherID             null.String `json:"VoucherID" gorm:"Column:VoucherID"`
	Terminal              null.String `json:"terminal" gorm:"Column:Terminal"`
}

// RequestOrder ...
type RequestOrder struct {
	//User data
	FirstName null.String `json:"FirstName"`
	LastName  null.String `json:"LastName" `
	Email     null.String `json:"Email" `
	Phone     null.String `json:"Phone" `
	Street    null.String `json:"Street" `
	City      null.String `json:"City" `
	State     null.String `json:"State" `
	Postcode  null.String `json:"Postcode" `
	Country   null.String `json:"Country"`
	UserKey   null.String `json:"UserKey"`

	//Product data
	Amount        null.Float64 `json:"Amount"`
	Currency      null.String  `json:"Currency"`
	Reference     null.String  `json:"Reference"`
	Type          null.String  `json:"Type"`
	ProductType   null.String  `json:"ProductType"`
	SKU           null.String  `json:"SKU"`
	RecurringFreq null.Int     `json:"RecurringFreq"`
	Installements null.Int     `json:"Installements"`
	Organization  null.String  `json:"Organization"`
	OrderLanguage null.String  `json:"OrderLanguage"`
	Quantity      null.Int     `json:"Quantity"`
	AmountItem    null.Int     `json:"AmountItem"`
	TerminalId    null.String  `json:"TerminalId"`

	//Transaction data
	SuccessURL null.String `json:"SuccessURL"`
	ErrorURL   null.String `json:"ErrorURL"`
	CancelURL  null.String `json:"CancelURL"`

	// Payment data
	PaymentType   null.String `json:"PaymentType,omitempty"`
	PaymentStatus null.String `json:"PaymentStatus,omitempty"`

	//Offline Payment
	PaymentMethod        null.String `json:"PaymentMethod,omitempty"`
	Receipt              null.String `json:"Receipt,omitempty"`
	ExtraInfo            null.String `json:"ExtraInfo,omitempty"`
	OfflinePaymentStatus null.String `json:"OfflinePaymentStatus,omitempty"`
	Properties           null.JSON   `json:"Properties,omitempty"`

	//Helphaver Payment
	ValidationMessage null.String `json:"ValidationMessage,omitempty"`
	RejectionMessage  null.String `json:"RejectionMessage,omitempty"`
}
type RequestUpdateToken struct {
	Token      string `json:"token"`
	OrderId    int    `json:"order_id"`
	ParamX     string `json:"param_x"`
	CardNumber string `json:"card_number"`
	CardExp    string `json:"card_exp"`
}

type RequestToken struct {
	//User data
	FirstName null.String `json:"FirstName"`
	LastName  null.String `json:"LastName" `
	Email     null.String `json:"Email" `
	Phone     null.String `json:"Phone" `
	Country   null.String `json:"Country"`
	UserKey   null.String `json:"UserKey"`

	//Product data
	Currency      null.String `json:"Currency"`
	Reference     null.String `json:"Reference"`
	VAT           null.String `json:"VAT"`
	SKU           null.String `json:"SKU"`
	Installements null.Int    `json:"Installements"`
	Organization  null.String `json:"Organization"`
	OrderLanguage null.String `json:"OrderLanguage"`

	//Transaction data
	SuccessURL null.String `json:"SuccessURL"`
	ErrorURL   null.String `json:"ErrorURL"`
	CancelURL  null.String `json:"CancelURL"`
}

type PaymentUpdate struct {
	//Common data
	PaymentType         null.String `json:"PaymentType"`
	Status              null.String `json:"Status"`
	RestrictOrderUpdate null.Bool   `json:"RestrictOrderUpdate"`
	PaymentID           null.Int    `json:"PaymentID"`

	//Offline Payment
	PaymentMethod null.String `json:"PaymentMethod"`
	Receipt       null.String `json:"Receipt"`
	ExtraInfo     null.String `json:"ExtraInfo"`
	Properties    null.JSON   `json:"Properties,omitempty"`

	// HelpHaver Payment
	ValidationMessage null.String `json:"ValidationMessage"`
	RejectionMessage  null.String `json:"RejectionMessage"`

	// Pelecard Payment
	DeletedAt null.Time `json:"deleted_at"`

	Amount null.Float64 `json:"Amount"`

	PaymentStatus null.String `json:"PaymentStatus"`
	OrderID       null.Int    `json:"OrderID"`

	ParamX          null.String `json:"additional_details_param_x"`
	Ordkey          null.String `json:"user_key"`
	AuthNo          null.String `json:"authNo"`
	ConfirmationKey null.String `json:"confirmation_key"`
	Success         null.String `json:"success"`
	PelecardToken   null.String `json:"token"`
	TransactionID   null.String `json:"transaction_id"`
	ErrorMsg        null.String `json:"ErrorMsg"`

	CardHebrewName   null.String `json:"card_hebrew_name"`
	CCAbroadCard     null.String `json:"CCAbroadCard"`
	CCBrand          null.String `json:"CCBrand"`
	CCCompanyClearer null.String `json:"CCCompanyClearer"`
	CCCompanyIssuer  null.String `json:"CCCompanyIssuer"`
	CreditType       null.String `json:"credit_type"`

	CCExpDate null.String `json:"CCExpDate"`
	CCNumber  null.String `json:"CCNumber"`

	DebitCode     null.String `json:"DebitCode"`
	DebitCurrency null.String `json:"DebitCurrency"`
	DebitTotal    null.String `json:"DebitTotal"`
	DebitType     null.String `json:"DebitType"`

	FirstPaymentTotal null.String `json:"FirstPaymentTotal"`
	FixedPaymentTotal null.String `json:"FixedPaymentTotal"`
	JParam            null.String `json:"j_param"`
	TotalPayments     null.String `json:"TotalPayments"`

	TransactionInitTime   null.String `json:"TransactionInitTime"`
	TransactionUpdateTime null.String `json:"TransactionUpdateTime"`
	VoucherID             null.String `json:"VoucherID"`
}

type OfflinePayment struct {
	ID        int       `json:"id"`
	CreatedAt null.Time `json:"created_at"`
	UpdatedAt null.Time `json:"updated_at"`
	DeletedAt null.Time `json:"deleted_at"`

	PaymentMethod null.String `json:"PaymentMethod"`
	Receipt       null.String `json:"Receipt"`
	ExtraInfo     null.String `json:"ExtraInfo"`
	Status        null.String `json:"Status"`
	PaymentID     null.Int    `json:"PaymentID"`
	Properties    null.JSON   `json:"Properties"`
}

type OfflinePaymentRequest struct {
	KeycloakID    null.String `json:"keycloak_id"`
	Currency      null.String `json:"currency"`
	Amount        float64     `json:"amount"`
	PaymentDate   null.Time   `json:"payment_date"`
	Language      null.String `json:"language"`
	PaymentMethod null.String `json:"payment_method"`
	Note          null.String `json:"note"`
	Quantity      int         `json:"quantity"`
}

type HelpHavedPayment struct {
	PaymentType       null.String `json:"PaymentType,omitempty"`
	Status            null.String `json:"Status"`
	PaymentID         null.Int    `json:"PaymentID"`
	ValidationMessage null.String `json:"ValidationMessage"`
	RejectionMessage  null.String `json:"RejectionMessage"`
}

// Account is defined by
type Account struct {
	ID        int        `json:"ID" gorm:"primary_key"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at" sql:"index"`

	FirstName null.String `json:"FirstName" gorm:"Column:FirstName;type:varchar(100)"`
	LastName  null.String `json:"LastName" gorm:"Column:LastName;type:varchar(100)"`
	Email     null.String `json:"Email" gorm:"Column:Email;type:varchar(100)"`
	Phone     null.String `json:"Phone" gorm:"Column:Phone;type:varchar(30)"`
	Street    null.String `json:"Street" gorm:"Column:Street;type:varchar(100)"`
	City      null.String `json:"City" gorm:"Column:City;type:varchar(85)"`
	State     null.String `json:"State" gorm:"Column:State;type:varchar(85)"`
	Postcode  null.String `json:"Postcode" gorm:"Column:Postcode;type:varchar(85)"`
	Country   null.String `json:"Country" gorm:"Column:Country;type:varchar(50)"`

	AccountType         null.String `json:"AccountType,omitempty" gorm:"Column:AccountType;type:varchar(100);default:'personal'"`
	PaymentToken        null.String `json:"PaymentToken,omitempty" gorm:"Column:PaymentToken;type:varchar(100)"`
	PaymentCardID       null.String `json:"PaymentCardID,omitempty" gorm:"Column:PaymentCardID;type:varchar(100)"`
	PaymentCardExpMonth null.Int    `json:"PaymentCardExpMonth,omitempty" gorm:"Column:PaymentCardExpMonth;type:int"`
	PaymentCardExpYear  null.Int    `json:"PaymentCardExpYear,omitempty" gorm:"Column:PaymentCardExpYear;type:int"`
	AuthNo              null.String `json:"authNo" gorm:"Column:AuthNo"`
	UserKey             null.String `json:"UserKCID,omitempty" gorm:"Column:UserKey;type:varchar(85)"`
}

type AccountMergeRequest struct {
	SourceKeycloakID      null.String `json:"source_keycloak_id"`
	DestinationKeycloakID null.String `json:"destination_keycloak_id"`
}

// RequestPayment ..
type RequestPayment struct {
	// Part for Pelecard
	GoodURL    string `json:"GoodURL"`
	ErrorURL   string `json:"ErrorURL"`
	CancelURL  string `json:"CancelURL"`
	ApprovalNo string `json:"ApprovalNo"`
	Token      string `json:"Token"`

	// Part for Priority
	Name         string  `json:"Name"`
	Price        float64 `json:"Price"`
	Currency     string  `json:"Currency"`
	Email        string  `json:"Email"`
	Phone        string  `json:"Phone"`
	Street       string  `json:"Street"`
	City         string  `json:"City"`
	Country      string  `json:"Country"`
	Participans  string  `json:"Participants"`
	Details      string  `json:"Details"`
	SKU          string  `json:"SKU"`
	VAT          string  `json:"VAT"`
	Installments int     `json:"Installments"`
	Language     string  `json:"Language"`
	Reference    string  `json:"Reference"`
	Organization string  `json:"Organization"`
	UserKey      string  `json:"UserKey"`
}

type RequestNewToken struct {
	// Part for Pelecard
	GoodURL   string `json:"GoodURL"`
	ErrorURL  string `json:"ErrorURL"`
	CancelURL string `json:"CancelURL"`

	// Part for Priority
	Name         string `json:"Name"`
	Currency     string `json:"Currency"`
	Email        string `json:"Email"`
	Phone        string `json:"Phone"`
	Country      string `json:"Country"`
	SKU          string `json:"SKU"`
	VAT          string `json:"VAT"`
	Installments int    `json:"Installments"`
	Language     string `json:"Language"`
	Reference    string `json:"Reference"`
	Organization string `json:"Organization"`
	UserKey      string `json:"UserKey"`
}

// RequestPaid ...
type RequestPaid struct {
	UserKey null.String `json:"user_key"`

	TransactionID   null.String `json:"transaction_id"`
	ParamX          null.String `json:"additional_details_param_x"`
	AuthNo          null.String `json:"authNo"`
	ConfirmationKey null.String `json:"confirmation_key"`
	Success         null.String `json:"success"`
	Token           null.String `json:"token"`
	Error           null.String `json:"error,omitempty"`

	CardHebrewName   null.String `json:"card_hebrew_name"`
	CCAbroadCard     null.String `json:"credit_card_abroad_card"`
	CCBrand          null.String `json:"credit_card_brand"`
	CCCompanyClearer null.String `json:"credit_card_company_clearer"`
	CCCompanyIssuer  null.String `json:"credit_card_company_issuer"`
	CreditType       null.String `json:"credit_type"`

	CCNumber  null.String `json:"credit_card_number"`
	CCExpDate null.String `json:"credit_card_exp_date"`

	DebitCode     null.String `json:"debit_code"`
	DebitCurrency null.String `json:"debit_currency"`
	DebitTotal    null.String `json:"debit_total"`
	DebitType     null.String `json:"debit_type"`

	FirstPaymentTotal null.String `json:"first_payment_total"`
	FixedPaymentTotal null.String `json:"fixed_payment_total"`
	JParam            null.String `json:"j_param"`
	TotalPayments     null.String `json:"total_payments"`

	TransactionInitTime   null.String `json:"transaction_init_time"`
	TransactionPelecardID null.String `json:"transaction_pelecard_id"`
	TransactionUpdateTime null.String `json:"transaction_update_time"`
	VoucherID             null.String `json:"voucher_id"`
}

// Product is storing all product info
//type Product struct {
//	//Product data
//	Descriptions  []ProductDescription `json:"ProductDescription"` // arranged by language
//	Cost          []Price              `json:"Cost"`               // arranged by currency
//	Type          string               `json:"Type"`
//	ProductType   string               `json:"ProductType"`
//	SKU           string               `json:"SKU"`
//	RecurringFreq int                  `json:"RecurringFreq"`
//	Installements int                  `json:"Installements"`
//	Organization  string               `json:"Organization"`
//}

// Price for multicurrent products
//type Price struct {
//	Currency string  `json:"currency"`
//	Fixed    bool    `json:"fixed"`
//	Amount   float64 `json:"amount"`
//	Min      int     `json:"min"`
//	Max      int     `json:"max"`
//	Step     int     `json:"step"`
//}

// ProductDescription specify product desc
//type ProductTypeuctDescription struct {
//	Locale     string      `json:"locale"`
//	Header     Description `json:"header"`
//	Body       Description `json:"body"`
//	TosURL     string      `json:"TosURL"`
//	CancelURL  string      `json:"CancelURL"`
//	CancelText string      `json:"CancelText"`
//	ButtonText string      `json:"ButtonText"`
//}

// Description generic  metadata
//type Description struct {
//	Title    string `json:"title"`
//	Subtitle string `json:"subtitle"`
//	Body     string `json:"body"`
//}

type OrderServiceEmvRes struct {
	Status string `json:"status"`
	URL    string `json:"url"`
	Error  string `json:"error"`
}

type PaymentWithFullName struct {
	AccountID   int    `json:"AccountID"`
	UserKey     string `json:"UserKey"`
	FirstName   string `json:"FirstName"`
	LastName    string `json:"LastName"`
	Email       string `json:"Email"`
	Street      string `json:"Street"`
	City        string `json:"City"`
	Language    string `json:"Language"`
	OrderAmount int    `json:"OrderAmount"`
	Currency    string `json:"Currency"`

	ID                    uint         `json:"ID"`
	Amount                null.Float64 `json:"Amount"`
	PaymentStatus         null.String  `json:"PaymentStatus"`
	PaymentType           null.String  `json:"PaymentType"`
	OrderID               null.Int     `json:"OrderID"`
	ParamX                null.String  `json:"additional_details_param_x"`
	SKU                   null.String  `json:"SKU"`
	AuthNo                null.String  `json:"authNo"`
	ConfirmationKey       null.String  `json:"confirmation_key"`
	Success               null.String  `json:"success"`
	PelecardToken         null.String  `json:"token"`
	TransactionID         null.String  `json:"transaction_id"`
	ErrorMsg              null.String  `json:"ErrorMsg"`
	CardHebrewName        null.String  `json:"card_hebrew_name"`
	CCAbroadCard          null.String  `json:"CCAbroadCard"`
	CCBrand               null.String  `json:"CCBrand"`
	CCCompanyClearer      null.String  `json:"CCCompanyClearer"`
	CCCompanyIssuer       null.String  `json:"CCCompanyIssuer"`
	CreditType            null.String  `json:"credit_type"`
	CCExpDate             null.String  `json:"CCExpDate"`
	CCNumber              null.String  `json:"CCNumber"`
	DebitCode             null.String  `json:"DebitCode"`
	DebitCurrency         null.String  `json:"DebitCurrency"`
	DebitTotal            null.String  `json:"DebitTotal"`
	DebitType             null.String  `json:"DebitType"`
	FirstPaymentTotal     null.String  `json:"FirstPaymentTotal"`
	FixedPaymentTotal     null.String  `json:"FixedPaymentTotal"`
	JParam                null.String  `json:"j_param"`
	TotalPayments         null.String  `json:"TotalPayments"`
	TransactionInitTime   null.String  `json:"TransactionInitTime"`
	TransactionUpdateTime null.String  `json:"TransactionUpdateTime"`
	VoucherID             null.String  `json:"VoucherID"`
	Ordkey                null.String  `json:"user_key"`
	CreatedAt             null.Time    `json:"-"`
	UpdatedAt             null.Time    `json:"-"`
	DeletedAt             null.Time    `json:"-"`
}

type PaymentByEmail struct {
	OrderID       null.Int     `json:"order_id"`
	CreatedAt     time.Time    `json:"created_at"`
	PaymentDate   null.Time    `json:"payment_date"`
	Type          null.String  `json:"type"`
	ProductType   null.String  `json:"product_type"`
	PaymentID     null.String  `json:"payment_id"`
	Currency      null.String  `json:"currency"`
	Amount        null.Float64 `json:"amount"`
	CCNumber      null.String  `json:"cc_number"`
	PaymentStatus null.String  `json:"payment_status"`
}

type PaymentActivitiesRes struct {
	CreatedAt     null.Time    `json:"created_at"`
	Amount        null.Float64 `json:"amount"`
	PaymentType   null.String  `json:"payment_type"`
	OrderID       null.Int     `json:"order_id"`
	ParamX        null.String  `json:"additional_details_param_x" gorm:"Column:ParamX"`
	PaymentStatus null.String  `json:"payment_status"`
	CCNumber      null.String  `json:"cc_number"`
	CCExpDate     null.String  `json:"cc_exp_date"`
	ProductType   null.String  `json:"product_type"`
	Type          null.String  `json:"type"`
	Currency      null.String  `json:"currency"`
	FirstName     null.String  `json:"first_name"`
	LastName      null.String  `json:"last_name"`
	Email         null.String  `json:"email"`
	Country       null.String  `json:"country"`
}

type CardDetails struct {
	ID              uint        `json:"id"`
	AccountID       null.Int    `json:"account_id"`
	GatewayProvider null.String `json:"gateway_provider"`
	CCNumber        null.String `json:"cc_number"`
	CCExpDate       null.String `json:"cc_expdate"`
	Active          null.Bool   `json:"active"`
	Token           null.String `json:"token"`
	CreatedAt       null.Time   `json:"created_at"`
	UpdatedAt       null.Time   `json:"updated_at"`
	DeletedAt       null.Time   `json:"deleted_at"`
}

type Transaction struct {
	ID         uint        `json:"id"`
	OrderID    null.Int    `json:"order_id"`
	PaymentID  null.Int    `json:"payment_id"`
	AccountID  null.Int    `json:"account_id"`
	TerminalID null.String `json:"terminal_id"`
	Status     null.Int    `json:"status"`
	CreatedAt  null.Time   `json:"created_at"`
	UpdatedAt  null.Time   `json:"updated_at"`
}

type Special struct {
	Id          null.Int    `json:"id" gorm:"primary_key"`
	KeycloakId  null.String `json:"keycloak_id"`
	Email       null.String `json:"email"`
	StartDate   null.Time   `json:"start_date"`
	EndDate     null.Time   `json:"end_date"`
	Category    null.String `json:"category"`
	SubCategory null.String `json:"subcategory"`
}

type SpecialKeycloakIdUpdate struct {
	KeycloakId string `json:"keycloak_id"`
	Email      string `json:"email"`
}

type OperationReq struct {
	ID            *int    `json:"id" form:"id"`
	NewEmail      *string `json:"new_email" form:"new_email" binding:"required"`
	OldEmail      *string `json:"old_email" form:"old_email" binding:"required"`
	NewKeycloakID *string `json:"new_keycloak_id" form:"new_keycloak_id"`
	OldKeycloakID *string `json:"old_keycloak_id" form:"old_keycloak_id"`
	Input         *string `json:"input"`
	Type          *string `json:"type" form:"type"`
	Output        *string `json:"output"`
	Status        *string `json:"status"`
	Revert        *string `json:"revert"`
}

type OperationTrace struct {
	ID     *int    `json:"id"`
	Input  *string `json:"input"`
	Output *string `json:"output"`
	Type   *string `json:"type"`
	Status *string `json:"status"`
	Revert *string `json:"revert"`
}

type PaidDetailC struct {
	TotalPeoplePaid       int64 `json:"total_people_paid"`
	TotalPeoplePaidWithCC int64 `json:"total_people_paid_with_cc"`
	TotalTicketSold       int64 `json:"total_ticket_sold"`
}

type UserMonthlyPriceRes struct {
	Amount         null.Float64 `json:"amount"`
	Currency       null.String  `json:"currency"`
	PricingVersion null.String  `json:"pricingVersion"`
}
