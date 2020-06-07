package main

import "time"

// Order is defined by
type Order struct {
	ID        uint       `json:"ID" gorm:"primary_key" fake:"skip"`
	CreatedAt time.Time  `json:"-" fake:"skip"`
	UpdatedAt time.Time  `json:"-" fake:"skip"`
	DeletedAt *time.Time `json:"-" sql:"index" fake:"skip"`

	Type         string `json:"type" gorm:"type:varchar(100)" fake:"{skip}"`
	ProductType  string `json:"ProductType" gorm:"type:varchar(100)" fake:"{skip}"`
	RecuringFreq int    `json:"RecuringFreq,omitempty" gorm:"type:int" sql:"DEFAULT:0" fake:"skip"`

	AccountID    uint   `json:"AccountID" fake:"{skip}"`
	Organization string `json:"Organization" gorm:"type:varchar(10)" fake:"skip"`

	Amount        float32 `json:"Amount" gorm:"type:varchar(85)" fake:"skip"`
	Currency      string  `json:"Currency"  gorm:"type:varchar(10)" fake:"{skip}"`
	Status        string  `json:"Status,omitempty" gorm:"type:varchar(85)" fake:"skip"`
	OrderLanguage string  `json:"OrderLanguage,omitempty" gorm:"type:varchar(10)" fake:"skip"`

	Payments []Payment `json:"Payments" gorm:"foreignkey:OrderID" fake:"{skip}"`
}

//Payment is defined by
type Payment struct {
	ID        uint       `json:"ID" gorm:"primary_key" fake:"skip"`
	CreatedAt time.Time  `json:"-" fake:"skip"`
	UpdatedAt time.Time  `json:"-" fake:"skip"`
	DeletedAt *time.Time `json:"-" sql:"index" fake:"skip"`

	Amount float32 `json:"Amount" fake:"{skip}"`

	Type    string `json:"type" gorm:"type:varchar(100)" fake:"{skip}"`
	OrderID uint   `json:"OrderID" fake:"{skip}" fake:"{skip}"`

	Invoices Invoice `gorm:"foreignkey:PaymentID" fake:"{skip}"`
}

//Invoice Details is defined by
type Invoice struct {
	ID        uint       `json:"ID" gorm:"primary_key" fake:"skip"`
	CreatedAt time.Time  `json:"-" fake:"skip"`
	UpdatedAt time.Time  `json:"-" fake:"skip"`
	DeletedAt *time.Time `json:"-" sql:"index" fake:"skip"`

	Firstname string `json:"Firstname" gorm:"type:varchar(100)" fake:"{firstname}"`
	Lastname  string `json:"Lastname" gorm:"type:varchar(100)" fake:"{lastname}"`
	Email     string `json:"Email" gorm:"type:varchar(100)" fake:"{email}"`
	Phone     string `json:"Phone" gorm:"type:varchar(30)" fake:"{phone}"`
	Street    string `json:"Street" gorm:"type:varchar(100)" fake:"{street}"`
	City      string `json:"City" gorm:"type:varchar(85)" fake:"{city}"`
	State     string `json:"State" gorm:"type:varchar(85)" fake:"{state}"`
	Postcode  string `json:"Postcode" gorm:"type:varchar(85)" fake:"{zipcode}"`
	Country   string `json:"Country" gorm:"type:varchar(50)" fake:"{country}"`

	OrderLanguage string `json:"PreferedLanguage" gorm:"type:varchar(10)" fake:"{languageabbreviation}"`

	PaymentID uint `json:"PaymentID" fake:"{skip}"`
}

//RequestOrder ...
type RequestOrder struct {
	//User data
	Firstname string `json:"Firstname"`
	Lastname  string `json:"Lastname" `
	Email     string `json:"Email" `
	Phone     string `json:"Phone" `
	Street    string `json:"Street" `
	City      string `json:"City" `
	State     string `json:"State" `
	Postcode  string `json:"Postcode" `
	Country   string `json:"Country"`
	UserKey   string `json:"UserKey"`

	//Product data
	Amount        float32 `json:"Amount"`
	Currency      string  `json:"Currency"`
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
	ID        uint       `json:"ID" gorm:"primary_key" fake:"skip"`
	CreatedAt time.Time  `json:"-" fake:"skip"`
	UpdatedAt time.Time  `json:"-" fake:"skip"`
	DeletedAt *time.Time `json:"-" sql:"index" fake:"skip"`

	Firstname string `json:"Firstname" gorm:"type:varchar(100)" fake:"{firstname}"`
	Lastname  string `json:"Lastname" gorm:"type:varchar(100)" fake:"{lastname}"`
	Email     string `json:"Email" gorm:"type:varchar(100)" fake:"{email}"`
	Phone     string `json:"Phone" gorm:"type:varchar(30)" fake:"{phone}"`
	Street    string `json:"Street" gorm:"type:varchar(100)" fake:"{street}"`
	City      string `json:"City" gorm:"type:varchar(85)" fake:"{city}"`
	State     string `json:"State" gorm:"type:varchar(85)" fake:"{state}"`
	Postcode  string `json:"Postcode" gorm:"type:varchar(85)" fake:"{zipcode}"`
	Country   string `json:"Country" gorm:"type:varchar(50)" fake:"{country}"`

	AccountType         string `json:"AccountType,omitempty" gorm:"type:varchar(100);default:'personal'" fake:"skip"`
	PaymentToken        string `json:"PaymentToken,omitempty" gorm:"type:varchar(100)" fake:"skip"`
	PaymentCardID       string `json:"PaymentCardID,omitempty" gorm:"type:varchar(100)" fake:"skip"`
	PaymentCardExpMonth int    `json:"PaymentCardExpMonth,omitempty" gorm:"type:int" fake:"skip"`
	PaymentCardExpYear  int    `json:"PaymentCardExpYear,omitempty" gorm:"type:int" fake:"skip"`
	UserKey             string `json:"UserKCID,omitempty" gorm:"type:varchar(85)" fake:"skip"`
}
