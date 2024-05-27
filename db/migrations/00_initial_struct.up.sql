BEGIN;

CREATE TABLE IF NOT EXISTS accounts
(
    id                      SERIAL,
    created_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at              TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at              TIMESTAMP WITH TIME ZONE DEFAULT null,
    "FirstName"             TEXT,
    "LastName"              TEXT,
    "Email"                 TEXT,
    "Phone"                 TEXT,
    "Street"                TEXT,
    "City"                  TEXT,
    "State"                 TEXT,
    "Postcode"              TEXT,
    "Country"               TEXT,
    "AccountType"           TEXT DEFAULT 'personal',
    "PaymentToken"          TEXT,
    "PaymentCardID"         TEXT,
    "PaymentCardExpMonth"   INT,
    "PaymentCardExpYear"    INT,
    "UserKey"               TEXT,
    "AuthNo"                TEXT,
    CONSTRAINT invoices_pkey PRIMARY KEY(id)
);


CREATE TABLE IF NOT EXISTS invoices
(
    id                          SERIAL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    "FirstName"                 TEXT,
    "LastName"                  TEXT,
    "Email"                     TEXT,
    "Phone"                     TEXT,
    "Street"                    TEXT,
    "City"                      TEXT,
    "State"                     TEXT,
    "Postcode"                  TEXT,
    "Country"                   TEXT,
    "OrderLanguage"             TEXT,
    "PaymentID"                 INT,
    CONSTRAINT accounts_pkey PRIMARY KEY(id)
);

CREATE TABLE IF NOT EXISTS orders
(
    id                          SERIAL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    "Type"                      TEXT,
    "ProductType"               TEXT,
    "RecuringFreq"              TEXT,
    "AccountID"                 INT,
    "Organization"              TEXT,
    "Amount"                    INT,
    "Currency"                  TEXT,
    "Status"                    TEXT,
    "OrderLanguage"             TEXT,
    "PaymentDate"               TIMESTAMP WITH TIME ZONE DEFAULT null,
    "SKU"                       TEXT,
    "Note"                      TEXT,
    "Flag"                      TEXT,
    userkey                     TEXT,
    CONSTRAINT orders_pkey PRIMARY KEY(id)
);

CREATE TABLE IF NOT EXISTS payments
(
    id                          SERIAL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    "Amount"                    INT,
    "PaymentStatus"             TEXT,
    "PaymentType"               TEXT,
    "OrderID"                   INT,
    "ParamX"                    TEXT,
    "AuthNo"                    TEXT,
    confirmation_key            TEXT,
    success                     TEXT,
    pelecard_token              TEXT,
    "TransactionID"             TEXT,
    "ErrorMsg"                  TEXT,
    "CardHebrewName"            TEXT,
    "CCAbroadCard"              TEXT,
    "CCBrand"                   TEXT,
    "CCCompanyClearer"          TEXT,
    "CCCompanyIssuer"           TEXT,
    credit_type                 TEXT,
    "CCExpDate"                 TEXT,
    "CCNumber"                  TEXT,
    "DebitCode"                 TEXT,
    "DebitCurrency"             TEXT,
    "DebitTotal"                TEXT,
    "DebitType"                 TEXT,
    "FirstPaymentTotal"         TEXT,
    "FixedPaymentTotal"         TEXT,
    j_param                     TEXT,
    "TotalPayments"             TEXT,
    "TransactionInitTime"       TEXT,
    "TransactionUpdateTime"     TEXT,
    "VoucherID"                 TEXT,
    "Ordkey"                    TEXT,
    CONSTRAINT payments_pkey PRIMARY KEY(id)
);

CREATE TABLE IF NOT EXISTS payments_helphaver
(
    id                          SERIAL,
    status                      TEXT,
    payment_id                  INT NOT NULL,
    validation_message          TEXT,
    rejection_message           TEXT,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT payments_helphaver_pkey PRIMARY KEY(id),
    CONSTRAINT fk_payment_id  FOREIGN KEY(payment_id) REFERENCES payments(id)
);

CREATE TABLE IF NOT EXISTS payments_offline
(
    id                          SERIAL,
    payment_method              TEXT NOT NULL,
    receipt                     TEXT,
    extra_info                  TEXT,
    status                      TEXT,
    payment_id                  INT NOT NULL,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT payments_offline_pkey PRIMARY KEY(id),
    CONSTRAINT fk_payment_id  FOREIGN KEY(payment_id) REFERENCES payments(id)
);

CREATE TABLE IF NOT EXISTS payments_pelecard
(
    id                          SERIAL,
    payment_id                  INT NOT NULL,
    amount                      INT,
    payment_status              TEXT,
    payment_type                TEXT,
    order_id                    INT,
    paramx                      TEXT,
    auth_no                     TEXT,
    confirmation_key            TEXT,
    success                     TEXT,
    pelecard_token              TEXT,
    transaction_id              TEXT,
    error_msg                   TEXT,
    cardhebrew_name             TEXT,
    cc_abroad_card              TEXT,
    cc_brand                    TEXT,
    cc_company_cleared          TEXT,
    cc_company_issue            TEXT,
    credit_type                 TEXT,
    cc_exp_date                 TEXT,
    cc_number                   TEXT,
    debit_code                  TEXT,
    debit_currency              TEXT,
    debit_total                 TEXT,
    debit_type                  TEXT,
    first_payment_total         TEXT,
    fixed_payment_total         TEXT,
    j_param                     TEXT,
    total_payments              TEXT,
    transaction_init_time       TEXT,
    transaction_update_time     TEXT,
    voucher_id                  TEXT,
    ord_key                     TEXT,
    created_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    updated_at                  TIMESTAMP WITH TIME ZONE DEFAULT now(),
    deleted_at                  TIMESTAMP WITH TIME ZONE DEFAULT null,
    CONSTRAINT payments_pelecard_pkey PRIMARY KEY(id),
    CONSTRAINT fk_payment_id FOREIGN KEY(payment_id) REFERENCES payments(id)
);

CREATE TABLE IF NOT EXISTS specials
(
    email                       TEXT NOT NULL,
    category                    TEXT NOT NULL,
    subcategory                 TEXT,
    CONSTRAINT specials_email_key UNIQUE(email)
);

COMMIT;