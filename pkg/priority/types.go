package priority

import "strings"

// IsActive returns true if the customer is considered active.
// Checks InactiveFlag first; falls back to StatDes Hebrew text.
func (c *Customer) IsActive() bool {
	if c.InactiveFlag != nil && *c.InactiveFlag == "Y" {
		return false
	}
	if strings.Contains(c.StatDes, "לא פעיל") {
		return false
	}
	return true
}

// Customer represents a customer record from Priority ERP
// Fields match the actual OData entity fields from Priority ERP API response
type Customer struct {
	CustName         string  `json:"CUSTNAME,omitempty"`         // Customer code/identifier
	CustDes          string  `json:"CUSTDES,omitempty"`          // Customer description/name (Hebrew)
	CustDesLong      *string `json:"CUSTDESLONG,omitempty"`      // Long customer description
	ECustDes         string  `json:"ECUSTDES,omitempty"`         // English customer description
	StatDes          string  `json:"STATDES,omitempty"`          // Status description
	OwnerLogin       string  `json:"OWNERLOGIN,omitempty"`       // Owner login
	InactiveFlag     *string `json:"INACTIVEFLAG,omitempty"`     // Inactive flag
	CreatedDate      string  `json:"CREATEDDATE,omitempty"`      // Creation date (ISO format)
	StatusDate       string  `json:"STATUSDATE,omitempty"`       // Status date (ISO format)
	Phone            string  `json:"PHONE,omitempty"`            // Phone number
	Fax              *string `json:"FAX,omitempty"`              // Fax number
	Email            string  `json:"EMAIL,omitempty"`            // Email address
	BusinessType     *string `json:"BUSINESSTYPE,omitempty"`     // Business type
	MCustName        *string `json:"MCUSTNAME,omitempty"`        // Master customer name
	MCustDes         *string `json:"MCUSTDES,omitempty"`         // Master customer description
	CTypeCode        string  `json:"CTYPECODE,omitempty"`        // Customer type code
	CTypeName        string  `json:"CTYPENAME,omitempty"`        // Customer type name
	PCustName        *string `json:"PCUSTNAME,omitempty"`        // Parent customer name
	CType2Code       *string `json:"CTYPE2CODE,omitempty"`       // Customer type 2 code
	PCustDes         *string `json:"PCUSTDES,omitempty"`         // Parent customer description
	CType2Name       *string `json:"CTYPE2NAME,omitempty"`       // Customer type 2 name
	CustPart         *string `json:"CUSTPART,omitempty"`         // Customer part
	NSFlag           string  `json:"NSFLAG,omitempty"`           // NS flag
	STCode           *string `json:"STCODE,omitempty"`           // ST code
	STDes            *string `json:"STDES,omitempty"`            // ST description
	ZoneCode         *string `json:"ZONECODE,omitempty"`         // Zone code
	ZoneDes          *string `json:"ZONEDES,omitempty"`          // Zone description
	Track            *string `json:"TRACK,omitempty"`            // Track
	CustNamePatName  *string `json:"CUSTNAMEPATNAME,omitempty"`  // Customer name pattern name
	Address          *string `json:"ADDRESS,omitempty"`          // Address line 1
	Address2         *string `json:"ADDRESS2,omitempty"`         // Address line 2
	Address3         *string `json:"ADDRESS3,omitempty"`         // Address line 3
	State            *string `json:"STATE,omitempty"`            // State
	StateA           *string `json:"STATEA,omitempty"`           // State A
	StateCode        *string `json:"STATECODE,omitempty"`        // State code
	StateName        *string `json:"STATENAME,omitempty"`        // State name
	Zip              *string `json:"ZIP,omitempty"`              // ZIP/Postal code
	CountryName      *string `json:"COUNTRYNAME,omitempty"`      // Country name
	WTaxNum          *string `json:"WTAXNUM,omitempty"`          // W tax number
	WTaxNumExpl      *string `json:"WTAXNUMEXPL,omitempty"`      // W tax number explanation
	VatNum           string  `json:"VATNUM,omitempty"`           // VAT/Tax number
	AgentCode        *string `json:"AGENTCODE,omitempty"`        // Agent code
	AgentName        *string `json:"AGENTNAME,omitempty"`        // Agent name
	AgentCode2       *string `json:"AGENTCODE2,omitempty"`       // Agent code 2
	AgentName2       *string `json:"AGENTNAME2,omitempty"`       // Agent name 2
	TerritoryCode    *string `json:"TERRITORYCODE,omitempty"`    // Territory code
	TerritoryDes     *string `json:"TERRITORYDES,omitempty"`     // Territory description
	Commission       float64 `json:"COMMISSION,omitempty"`       // Commission
	DSIMembers       int     `json:"DSI_MEMBERS,omitempty"`      // DSI members
	Established      *string `json:"ESTABLISHED,omitempty"`      // Established date
	EmpNum           int     `json:"EMPNUM,omitempty"`           // Employee number
	BranchName       *string `json:"BRANCHNAME,omitempty"`       // Branch name
	BranchDes        *string `json:"BRANCHDES,omitempty"`        // Branch description
	PayCode          *string `json:"PAYCODE,omitempty"`          // Payment code
	PayDes           *string `json:"PAYDES,omitempty"`           // Payment description
	MaxCredit        float64 `json:"MAX_CREDIT,omitempty"`       // Maximum credit
	MaxObligo        float64 `json:"MAX_OBLIGO,omitempty"`       // Maximum obligo
	OBCode           string  `json:"OBCODE,omitempty"`           // OB code
	DistrLineCode    *string `json:"DISTRLINECODE,omitempty"`    // Distribution line code
	DistrLineDes     *string `json:"DISTRLINEDES,omitempty"`     // Distribution line description
	UnloadTime       string  `json:"UNLOADTIME,omitempty"`       // Unload time
	DistrOrder       int     `json:"DISTRORDER,omitempty"`       // Distribution order
	NotAllowForecast *string `json:"NOTALLOWFORECAST,omitempty"` // Not allow forecast
	RecyclingFlag    *string `json:"RECYCLINGFLAG,omitempty"`    // Recycling flag
	BonusFlag        *string `json:"BONUSFLAG,omitempty"`        // Bonus flag
	CompetitorFlag   *string `json:"COMPETITORFLAG,omitempty"`   // Competitor flag
	Forecast         *string `json:"FORECAST,omitempty"`         // Forecast
	Chanel           *string `json:"CHANEL,omitempty"`           // Channel
	DistrTypeCode    *string `json:"DISTRTYPECODE,omitempty"`    // Distribution type code
	DistrTypeDes     *string `json:"DISTRTYPEDES,omitempty"`     // Distribution type description
	SecondLangText   *string `json:"SECONDLANGTEXT,omitempty"`   // Second language text
	Confidential     *string `json:"CONFIDENTIAL,omitempty"`     // Confidential
	HostName         *string `json:"HOSTNAME,omitempty"`         // Host name
	Spec1            *string `json:"SPEC1,omitempty"`            // Special field 1
	Spec2            *string `json:"SPEC2,omitempty"`            // Special field 2
	Spec3            *string `json:"SPEC3,omitempty"`            // Special field 3
	Spec4            *string `json:"SPEC4,omitempty"`            // Special field 4
	Spec5            *string `json:"SPEC5,omitempty"`            // Special field 5
	Spec6            *string `json:"SPEC6,omitempty"`            // Special field 6
	Spec7            *string `json:"SPEC7,omitempty"`            // Special field 7
	Spec8            *string `json:"SPEC8,omitempty"`            // Special field 8
	Spec9            *string `json:"SPEC9,omitempty"`            // Special field 9
	Spec10           *string `json:"SPEC10,omitempty"`           // Special field 10
	Spec11           *string `json:"SPEC11,omitempty"`           // Special field 11
	Spec12           *string `json:"SPEC12,omitempty"`           // Special field 12
	Spec13           *string `json:"SPEC13,omitempty"`           // Special field 13
	Spec14           *string `json:"SPEC14,omitempty"`           // Special field 14
	Spec15           *string `json:"SPEC15,omitempty"`           // Special field 15
	Spec16           *string `json:"SPEC16,omitempty"`           // Special field 16
	Spec17           *string `json:"SPEC17,omitempty"`           // Special field 17
	Spec18           *string `json:"SPEC18,omitempty"`           // Special field 18
	Spec19           *string `json:"SPEC19,omitempty"`           // Special field 19
	Spec20           *string `json:"SPEC20,omitempty"`           // Special field 20
	ExtFileFlag      *string `json:"EXTFILEFLAG,omitempty"`      // External file flag
	WaveStrategyCode *string `json:"WAVESTRATEGYCODE,omitempty"` // Wave strategy code
	WaveStrategyDes  *string `json:"WAVESTRATEGYDES,omitempty"`  // Wave strategy description
	PickStgCode      *string `json:"PICKSTGCODE,omitempty"`      // Pick staging code
	PickStgDes       *string `json:"PICKSTGDES,omitempty"`       // Pick staging description
	AutoShpFlag      *string `json:"AUTOSHPFLAG,omitempty"`      // Auto ship flag
	EDocuments       string  `json:"EDOCUMENTS,omitempty"`       // E-documents flag
	GPSX             *string `json:"GPSX,omitempty"`             // GPS X coordinate
	GPSY             *string `json:"GPSY,omitempty"`             // GPS Y coordinate
	QRankCode        *string `json:"QRANKCODE,omitempty"`        // Q rank code
	QRankDes         *string `json:"QRANKDES,omitempty"`         // Q rank description
	DCMonths         int     `json:"DCMONTHS,omitempty"`         // DC months
	Code             string  `json:"CODE,omitempty"`             // Code
	TaxCode          string  `json:"TAXCODE,omitempty"`          // Tax code
	TaxDes           string  `json:"TAXDES,omitempty"`           // Tax description
	VATCountryName   *string `json:"VATCOUNTRYNAME,omitempty"`   // VAT country name
	CustRemark       *string `json:"CUSTREMARK,omitempty"`       // Customer remark
	PikOrder         int     `json:"PIKORDER,omitempty"`         // Pick order
	MinExpDays       int     `json:"MINEXPDAYS,omitempty"`       // Minimum expiration days
	ReservFlag       *string `json:"RESERVFLAG,omitempty"`       // Reservation flag
	Lang             int     `json:"LANG,omitempty"`             // Language
	ROTLTCPTag       *string `json:"ROTL_TCPTAG,omitempty"`      // ROTL TCP tag
	LangName         *string `json:"LANGNAME,omitempty"`         // Language name
	ROTLVegan        *string `json:"ROTL_VEGAN,omitempty"`       // ROTL vegan
	EORINum          *string `json:"EORINUM,omitempty"`          // EORI number
	ROTLDebit        *string `json:"ROTL_DEBIT,omitempty"`       // ROTL debit
	ReturnLabels     *string `json:"RETURNLABELS,omitempty"`     // Return labels
	QAmtEDocuments   string  `json:"QAMT_EDOCUMENTS,omitempty"`  // Q amount e-documents
	ROTLBirthDate    *string `json:"ROTL_BIRTHDATE,omitempty"`   // ROTL birth date
	DSIBuildingC     *string `json:"DSI_BUILDING_C,omitempty"`   // DSI building C
	DSIBuildingD     *string `json:"DSI_BUILDING_D,omitempty"`   // DSI building D
	DSIUserLogin     string  `json:"DSI_USERLOGIN,omitempty"`    // DSI user login
	DSIUDate         string  `json:"DSI_UDATE,omitempty"`        // DSI update date (ISO format)
	Cust             int     `json:"CUST,omitempty"`             // Customer ID
}

// CustomerODataResponse represents the standard OData response structure for customers
type CustomerODataResponse struct {
	Value []Customer `json:"value"`
	// OData metadata fields
	ODataContext  string `json:"@odata.context,omitempty"`
	ODataCount    int    `json:"@odata.count,omitempty"`
	ODataNextLink string `json:"@odata.nextLink,omitempty"`
}

// AccountReceivableItem represents an account receivable item from Priority ERP
// Fields match the actual OData entity fields from ACCFNCITEMS2_SUBFORM
type AccountReceivableItem struct {
	BALDATE      string  `json:"BALDATE,omitempty"`      // Balance date (ISO format)
	FNCNUM       string  `json:"FNCNUM,omitempty"`       // Financial number
	TODOREF      *string `json:"TODOREF,omitempty"`      // TODO reference
	FNCPATNAME   string  `json:"FNCPATNAME,omitempty"`   // Financial pattern name
	DETAILS      string  `json:"DETAILS,omitempty"`      // Details
	DEBIT        float64 `json:"DEBIT,omitempty"`        // Debit amount
	CREDIT       float64 `json:"CREDIT,omitempty"`       // Credit amount
	BAL          float64 `json:"BAL,omitempty"`          // Balance
	CODE         string  `json:"CODE,omitempty"`         // Code
	FNCREF2      *string `json:"FNCREF2,omitempty"`      // Financial reference 2
	FNCDATE      string  `json:"FNCDATE,omitempty"`      // Financial date (ISO format)
	CURDATE      string  `json:"CURDATE,omitempty"`      // Current date (ISO format)
	FNCLOTNUM    *string `json:"FNCLOTNUM,omitempty"`    // Financial lot number
	FNCIREF1     *string `json:"FNCIREF1,omitempty"`     // Financial item reference 1
	FNCIREF2     *string `json:"FNCIREF2,omitempty"`     // Financial item reference 2
	ACCNAME      string  `json:"ACCNAME,omitempty"`      // Account name
	ACCDES       string  `json:"ACCDES,omitempty"`       // Account description
	CASHFLOWCODE *string `json:"CASHFLOWCODE,omitempty"` // Cash flow code
	ERECONNUM    int     `json:"ERECONNUM,omitempty"`    // E reconciliation number
	FRECONNUM    int     `json:"FRECONNUM,omitempty"`    // F reconciliation number
	DEBIT1       float64 `json:"DEBIT1,omitempty"`       // Debit 1
	CREDIT1      float64 `json:"CREDIT1,omitempty"`      // Credit 1
	DEBIT2       float64 `json:"DEBIT2,omitempty"`       // Debit 2
	CREDIT2      float64 `json:"CREDIT2,omitempty"`      // Credit 2
	DEBIT3       float64 `json:"DEBIT3,omitempty"`       // Debit 3
	CREDIT3      float64 `json:"CREDIT3,omitempty"`      // Credit 3
	CODE3        *string `json:"CODE3,omitempty"`        // Code 3
	FNCICODE     *string `json:"FNCICODE,omitempty"`     // Financial item code
	COSTCNAME    *string `json:"COSTCNAME,omitempty"`    // Cost center name
	COSTCDES     *string `json:"COSTCDES,omitempty"`     // Cost center description
	COSTCNAME2   *string `json:"COSTCNAME2,omitempty"`   // Cost center name 2
	COSTCDES2    *string `json:"COSTCDES2,omitempty"`    // Cost center description 2
	COSTCNAME3   *string `json:"COSTCNAME3,omitempty"`   // Cost center name 3
	COSTCDES3    *string `json:"COSTCDES3,omitempty"`    // Cost center description 3
	COSTCNAME4   *string `json:"COSTCNAME4,omitempty"`   // Cost center name 4
	COSTCDES4    *string `json:"COSTCDES4,omitempty"`    // Cost center description 4
	COSTCNAME5   *string `json:"COSTCNAME5,omitempty"`   // Cost center name 5
	COSTCDES5    *string `json:"COSTCDES5,omitempty"`    // Cost center description 5
	BUDCODE      *string `json:"BUDCODE,omitempty"`      // Budget code
	BUDNAME      *string `json:"BUDNAME,omitempty"`      // Budget name
	QUANT1       float64 `json:"QUANT1,omitempty"`       // Quantity 1
	CUSTNAME     *string `json:"CUSTNAME,omitempty"`     // Customer name
	VATDATE      *string `json:"VATDATE,omitempty"`      // VAT date (ISO format)
	FNCTRANS     int     `json:"FNCTRANS,omitempty"`     // Financial transaction
	KLINE        int     `json:"KLINE,omitempty"`        // K line
}

// AccountReceivableODataResponse represents the standard OData response structure for account receivables
type AccountReceivableODataResponse struct {
	Value []AccountReceivableItem `json:"value"`
	// OData metadata fields
	ODataContext  string `json:"@odata.context,omitempty"`
	ODataCount    int    `json:"@odata.count,omitempty"`
	ODataNextLink string `json:"@odata.nextLink,omitempty"`
}
