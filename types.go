package main

import (
	"time"
)

// Order is defined by
type Order struct {
	ID        uint       `json:"ID" gorm:"primary_key"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `json:"-" sql:"index"`

	Type         string `json:"type" gorm:"Column:Type;type:varchar(100)"`
	ProductType  string `json:"ProductType" gorm:"Column:ProductType;type:varchar(100)"`
	RecuringFreq int    `json:"RecuringFreq,omitempty" gorm:"Column:RecuringFreq;type:int" sql:"DEFAULT:0"`

	AccountID    uint   `json:"AccountID" gorm:"Column:AccountID;"`
	Organization string `json:"Organization" gorm:"Column:Organization;type:varchar(10)"`

	Amount        float64   `json:"Amount" gorm:"Column:Amount;type:varchar(85)"`
	Currency      string    `json:"Currency"  gorm:"Column:Currency;type:varchar(10)"`
	SKU           string    `json:"SKU"  gorm:"Column:SKU;type:varchar(30)"`
	Status        string    `json:"Status,omitempty" gorm:"Column:Status;type:varchar(85)"`
	OrderLanguage string    `json:"OrderLanguage,omitempty" gorm:"Column:OrderLanguage;type:varchar(10)"`
	PaymentDate   time.Time `json:"-" gorm:"Column:PaymentDate"`
	Note          string    `json:"-" gorm:"Column:Note;type:varchar(200)"`
	Flag          string    `json:"-" gorm:"Column:Flag;type:varchar(200)"`

	Payments []Payment `json:"Payments" gorm:"foreignkey:OrderID"`
}

//Payment is defined by
type Payment struct {
	ID        uint       `json:"ID" gorm:"primary_key"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `json:"-" sql:"index"`

	Amount float64 `json:"Amount" gorm:"Column:Amount"`

	PaymentStatus string `json:"PaymentStatus" gorm:"Column:PaymentStatus"`
	PaymentType   string `json:"PaymentType" gorm:"Column:PaymentType;type:varchar(100)"`
	OrderID       uint   `json:"OrderID" gorm:"Column:OrderID"`

	ParamX          string  `json:"additional_details_param_x" gorm:"Column:ParamX"`
	Ordkey          string  `json:"user_key" gorm:"Column:Ordkey"`
	AuthNo          *string `json:"authNo" gorm:"Column:AuthNo"`
	ConfirmationKey string  `json:"confirmation_key" gorm:"ConfirmationKey"`
	Success         string  `json:"success" gorm:"Success"`
	PelecardToken   string  `json:"token" gorm:"PelecardToken"`
	TransactionID   string  `json:"transaction_id" gorm:"Column:TransactionID"`
	ErrorMsg        string  `json:"ErrorMsg" gorm:"Column:ErrorMsg"`

	CardHebrewName   string `json:"card_hebrew_name" gorm:"Column:CardHebrewName"`
	CCAbroadCard     string `json:"CCAbroadCard" gorm:"Column:CCAbroadCard"`
	CCBrand          string `json:"CCBrand" gorm:"Column:CCBrand"`
	CCCompanyClearer string `json:"CCCompanyClearer" gorm:"Column:CCCompanyClearer"`
	CCCompanyIssuer  string `json:"CCCompanyIssuer" gorm:"Column:CCCompanyIssuer"`
	CreditType       string `json:"credit_type" gorm:"CreditType"`

	CCExpDate string `json:"CCExpDate" gorm:"Column:CCExpDate"`
	CCNumber  string `json:"CCNumber" gorm:"Column:CCNumber"`

	DebitCode     string `json:"DebitCode" gorm:"Column:DebitCode"`
	DebitCurrency string `json:"DebitCurrency" gorm:"Column:DebitCurrency"`
	DebitTotal    string `json:"DebitTotal" gorm:"Column:DebitTotal"`
	DebitType     string `json:"DebitType" gorm:"Column:DebitType"`

	FirstPaymentTotal string `json:"FirstPaymentTotal" gorm:"Column:FirstPaymentTotal"`
	FixedPaymentTotal string `json:"FixedPaymentTotal" gorm:"Column:FixedPaymentTotal"`
	JParam            string `json:"j_param"`
	TotalPayments     string `json:"TotalPayments" gorm:"Column:TotalPayments"`

	TransactionInitTime   string `json:"TransactionInitTime" gorm:"Column:TransactionInitTime"`
	TransactionUpdateTime string `json:"TransactionUpdateTime" gorm:"Column:TransactionUpdateTime"`
	VoucherID             string `json:"VoucherID" gorm:"Column:VoucherID"`

	Invoices []Invoice `gorm:"foreignkey:PaymentID"`
}

//Invoice Details is defined by
type Invoice struct {
	ID        uint       `json:"ID" gorm:"primary_key"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `json:"-" sql:"index"`

	FirstName string `json:"FirstName" gorm:"Column:FirstName;type:varchar(100)"`
	LastName  string `json:"LastName" gorm:"Column:LastName;type:varchar(100)"`
	Email     string `json:"Email" gorm:"Column:Email;type:varchar(100)"`
	Phone     string `json:"Phone" gorm:"Column:Phone;type:varchar(30)"`
	Street    string `json:"Street" gorm:"Column:Street;type:varchar(100)"`
	City      string `json:"City" gorm:"Column:City;type:varchar(85)"`
	State     string `json:"State" gorm:"Column:State;type:varchar(85)"`
	Postcode  string `json:"Postcode" gorm:"Column:Postcode;type:varchar(85)"`
	Country   string `json:"Country" gorm:"Column:Country;type:varchar(50)"`

	OrderLanguage string `json:"OrderLanguage" gorm:"Column:OrderLanguage;type:varchar(10)"`

	PaymentID uint `json:"PaymentID" gorm:"Column:PaymentID"`
}

//RequestOrder ...
type RequestOrder struct {
	//User data
	FirstName string `json:"FirstName"`
	LastName  string `json:"LastName" `
	Email     string `json:"Email" `
	Phone     string `json:"Phone" `
	Street    string `json:"Street" `
	City      string `json:"City" `
	State     string `json:"State" `
	Postcode  string `json:"Postcode" `
	Country   string `json:"Country"`
	UserKey   string `json:"UserKey"`

	//Product data
	Amount        float64 `json:"Amount"`
	Currency      string  `json:"Currency"`
	Reference     string  `json:"Reference"`
	Type          string  `json:"Type"`
	ProductType   string  `json:"ProductType"`
	SKU           string  `json:"SKU"`
	RecurringFreq int     `json:"RecurringFreq"`
	Installements int     `json:"Installements"`
	Organization  string  `json:"Organization"`
	OrderLanguage string  `json:"OrderLanguage"`

	//Transaction data
	SuccessURL string `json:"SuccessURL"`
	ErrorURL   string `json:"ErrorURL"`
	CancelURL  string `json:"CancelURL"`
}

// Account is defined by
type Account struct {
	ID        uint       `json:"ID" gorm:"primary_key"`
	CreatedAt time.Time  `json:"-"`
	UpdatedAt time.Time  `json:"-"`
	DeletedAt *time.Time `json:"-" sql:"index"`

	FirstName string `json:"FirstName" gorm:"Column:FirstName;type:varchar(100)"`
	LastName  string `json:"LastName" gorm:"Column:LastName;type:varchar(100)"`
	Email     string `json:"Email" gorm:"Column:Email;type:varchar(100)"`
	Phone     string `json:"Phone" gorm:"Column:Phone;type:varchar(30)"`
	Street    string `json:"Street" gorm:"Column:Street;type:varchar(100)"`
	City      string `json:"City" gorm:"Column:City;type:varchar(85)"`
	State     string `json:"State" gorm:"Column:State;type:varchar(85)"`
	Postcode  string `json:"Postcode" gorm:"Column:Postcode;type:varchar(85)"`
	Country   string `json:"Country" gorm:"Column:Country;type:varchar(50)"`

	AccountType         string  `json:"AccountType,omitempty" gorm:"Column:AccountType;type:varchar(100);default:'personal'"`
	PaymentToken        string  `json:"PaymentToken,omitempty" gorm:"Column:PaymentToken;type:varchar(100)"`
	PaymentCardID       string  `json:"PaymentCardID,omitempty" gorm:"Column:PaymentCardID;type:varchar(100)"`
	PaymentCardExpMonth int     `json:"PaymentCardExpMonth,omitempty" gorm:"Column:PaymentCardExpMonth;type:int"`
	PaymentCardExpYear  int     `json:"PaymentCardExpYear,omitempty" gorm:"Column:PaymentCardExpYear;type:int"`
	AuthNo              *string `json:"authNo" gorm:"Column:AuthNo"`
	UserKey             string  `json:"UserKCID,omitempty" gorm:"Column:UserKey;type:varchar(85)"`
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

//RequestPaid ...
type RequestPaid struct {
	UserKey string `json:"user_key"`

	TransactionID   string  `json:"transaction_id"`
	ParamX          string  `json:"additional_details_param_x"`
	AuthNo          *string `json:"authNo"`
	ConfirmationKey string  `json:"confirmation_key"`
	Success         string  `json:"success"`
	Token           string  `json:"token"`
	Error           string  `json:"error,omitempty"`

	CardHebrewName   string `json:"card_hebrew_name"`
	CCAbroadCard     string `json:"credit_card_abroad_card"`
	CCBrand          string `json:"credit_card_brand"`
	CCCompanyClearer string `json:"credit_card_company_clearer"`
	CCCompanyIssuer  string `json:"credit_card_company_issuer"`
	CreditType       string `json:"credit_type"`

	CCNumber  string `json:"credit_card_number"`
	CCExpDate string `json:"credit_card_exp_date"`

	DebitCode     string `json:"debit_code"`
	DebitCurrency string `json:"debit_currency"`
	DebitTotal    string `json:"debit_total"`
	DebitType     string `json:"debit_type"`

	FirstPaymentTotal string `json:"first_payment_total"`
	FixedPaymentTotal string `json:"fixed_payment_total"`
	JParam            string `json:"j_param"`
	TotalPayments     string `json:"total_payments"`

	TransactionInitTime   string `json:"transaction_init_time"`
	TransactionPelecardID string `json:"transaction_pelecard_id"`
	TransactionUpdateTime string `json:"transaction_update_time"`
	VoucherID             string `json:"voucher_id"`
}

//Product is storing all product info
type Product struct {
	//Product data
	Descriptions  []ProductDescription `json:"ProductDescription"` // arranged by language
	Cost          []Price              `json:"Cost"`               // arranged by currency
	Type          string               `json:"Type"`
	ProductType   string               `json:"ProductType"`
	SKU           string               `json:"SKU"`
	RecurringFreq int                  `json:"RecurringFreq"`
	Installements int                  `json:"Installements"`
	Organization  string               `json:"Organization"`
}

//Price for multicurrent products
type Price struct {
	Currency string  `json:"currency"`
	Fixed    bool    `json:"fixed"`
	Amount   float64 `json:"amount"`
	Min      int     `json:"min"`
	Max      int     `json:"max"`
	Step     int     `json:"step"`
}

//ProductDescription specify product desc
type ProductDescription struct {
	Locale     string      `json:"locale"`
	Header     Description `json:"header"`
	Body       Description `json:"body"`
	TosURL     string      `json:"TosURL"`
	CancelURL  string      `json:"CancelURL"`
	CancelText string      `json:"CancelText"`
	ButtonText string      `json:"ButtonText"`
}

//Description generic  metadata
type Description struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Body     string `json:"body"`
}

type OrderServiceEmvRes struct {
	Status string `json:"status"`
	URL    string `json:"url"`
	Error  string `json:"error"`
}
