--
-- PostgreSQL database dump
--

-- Dumped from database version 12.12 (Debian 12.12-1.pgdg110+1)
-- Dumped by pg_dump version 13.1 (Ubuntu 13.1-1.pgdg18.04+1)

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: accounts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.accounts (
    id integer NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    "FirstName" character varying(100),
    "LastName" character varying(100),
    "Email" character varying(100),
    "Phone" character varying(30),
    "Street" character varying(100),
    "City" character varying(85),
    "State" character varying(85),
    "Postcode" character varying(85),
    "Country" character varying(50),
    "AccountType" character varying(100) DEFAULT 'personal'::character varying,
    "PaymentToken" character varying(100),
    "PaymentCardID" character varying(100),
    "PaymentCardExpMonth" integer,
    "PaymentCardExpYear" integer,
    "UserKey" character varying(85),
    "AuthNo" text
);


--
-- Name: accounts_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.accounts_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: accounts_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.accounts_id_seq OWNED BY public.accounts.id;


--
-- Name: cards; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.cards (
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    deleted_at timestamp with time zone,
    id integer NOT NULL,
    gateway text,
    token text,
    card_number text,
    card_exp text,
    card_exp_month integer,
    card_exp_year integer,
    accountid integer
);


--
-- Name: cards_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.cards_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: cards_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.cards_id_seq OWNED BY public.cards.id;


--
-- Name: invoices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoices (
    id integer NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    "FirstName" character varying(100),
    "LastName" character varying(100),
    "Email" character varying(100),
    "Phone" character varying(30),
    "Street" character varying(100),
    "City" character varying(85),
    "State" character varying(85),
    "Postcode" character varying(85),
    "Country" character varying(50),
    "OrderLanguage" character varying(10),
    "PaymentID" integer
);


--
-- Name: invoices_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.invoices_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: invoices_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.invoices_id_seq OWNED BY public.invoices.id;


--
-- Name: orders; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.orders (
    id integer NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    "Type" character varying(100),
    "ProductType" character varying(100),
    "RecuringFreq" integer DEFAULT 0,
    "AccountID" integer,
    "Organization" character varying(10),
    "Amount" character varying(85),
    "Currency" character varying(10),
    "Status" character varying(85),
    "OrderLanguage" character varying(10),
    "PaymentDate" timestamp with time zone,
    "SKU" character varying(30),
    "Note" character varying(200),
    "Flag" character varying(200),
    userkey character varying(85)
);


--
-- Name: orders_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.orders_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: orders_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.orders_id_seq OWNED BY public.orders.id;


--
-- Name: payments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments (
    id integer NOT NULL,
    created_at timestamp with time zone,
    updated_at timestamp with time zone,
    deleted_at timestamp with time zone,
    "Amount" numeric,
    "PaymentStatus" text,
    "PaymentType" character varying(100),
    "OrderID" integer,
    "ParamX" text,
    "AuthNo" text,
    confirmation_key text,
    success text,
    pelecard_token text,
    "TransactionID" text,
    "ErrorMsg" text,
    "CardHebrewName" text,
    "CCAbroadCard" text,
    "CCBrand" text,
    "CCCompanyClearer" text,
    "CCCompanyIssuer" text,
    credit_type text,
    "CCExpDate" text,
    "CCNumber" text,
    "DebitCode" text,
    "DebitCurrency" text,
    "DebitTotal" text,
    "DebitType" text,
    "FirstPaymentTotal" text,
    "FixedPaymentTotal" text,
    j_param text,
    "TotalPayments" text,
    "TransactionInitTime" text,
    "TransactionUpdateTime" text,
    "VoucherID" text,
    "Ordkey" text
);


--
-- Name: payments_helphaver; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments_helphaver (
    id integer NOT NULL,
    status text,
    payment_id integer NOT NULL,
    validation_message text,
    rejection_message text,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    deleted_at timestamp with time zone
);


--
-- Name: payments_helphaver_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.payments_helphaver_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: payments_helphaver_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.payments_helphaver_id_seq OWNED BY public.payments_helphaver.id;


--
-- Name: payments_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.payments_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: payments_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.payments_id_seq OWNED BY public.payments.id;


--
-- Name: payments_offline; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments_offline (
    id integer NOT NULL,
    payment_method text NOT NULL,
    receipt text,
    extra_info text,
    status text,
    payment_id integer NOT NULL,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    deleted_at timestamp with time zone
);


--
-- Name: payments_offline_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.payments_offline_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: payments_offline_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.payments_offline_id_seq OWNED BY public.payments_offline.id;


--
-- Name: payments_pelecard; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments_pelecard (
    id integer NOT NULL,
    payment_id integer NOT NULL,
    amount integer,
    payment_status text,
    payment_type text,
    order_id integer,
    paramx text,
    auth_no text,
    confirmation_key text,
    success text,
    pelecard_token text,
    transaction_id text,
    error_msg text,
    cardhebrew_name text,
    cc_abroad_card text,
    cc_brand text,
    cc_company_clearer text,
    cc_company_issuer text,
    credit_type text,
    cc_exp_date text,
    cc_number text,
    debit_code text,
    debit_currency text,
    debit_total text,
    debit_type text,
    first_payment_total text,
    fixed_payment_total text,
    j_param text,
    total_payments text,
    transaction_init_time text,
    transaction_update_time text,
    voucher_id text,
    ord_key text,
    created_at timestamp with time zone DEFAULT now(),
    updated_at timestamp with time zone DEFAULT now(),
    deleted_at timestamp with time zone
);


--
-- Name: payments_pelecard_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.payments_pelecard_id_seq
    AS integer
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: payments_pelecard_id_seq; Type: SEQUENCE OWNED BY; Schema: public; Owner: -
--

ALTER SEQUENCE public.payments_pelecard_id_seq OWNED BY public.payments_pelecard.id;


--
-- Name: specials; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.specials (
    email character varying(50) NOT NULL,
    category character varying(50) NOT NULL,
    subcategory character varying(50)
);


--
-- Name: specials_sep2021; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.specials_sep2021 (
    email character varying(50),
    category character varying(50),
    subcategory character varying(50)
);


--
-- Name: accounts id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accounts ALTER COLUMN id SET DEFAULT nextval('public.accounts_id_seq'::regclass);


--
-- Name: cards id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cards ALTER COLUMN id SET DEFAULT nextval('public.cards_id_seq'::regclass);


--
-- Name: invoices id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoices ALTER COLUMN id SET DEFAULT nextval('public.invoices_id_seq'::regclass);


--
-- Name: orders id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders ALTER COLUMN id SET DEFAULT nextval('public.orders_id_seq'::regclass);


--
-- Name: payments id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments ALTER COLUMN id SET DEFAULT nextval('public.payments_id_seq'::regclass);


--
-- Name: payments_helphaver id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_helphaver ALTER COLUMN id SET DEFAULT nextval('public.payments_helphaver_id_seq'::regclass);


--
-- Name: payments_offline id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_offline ALTER COLUMN id SET DEFAULT nextval('public.payments_offline_id_seq'::regclass);


--
-- Name: payments_pelecard id; Type: DEFAULT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_pelecard ALTER COLUMN id SET DEFAULT nextval('public.payments_pelecard_id_seq'::regclass);


--
-- Name: accounts accounts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.accounts
    ADD CONSTRAINT accounts_pkey PRIMARY KEY (id);


--
-- Name: cards cards_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.cards
    ADD CONSTRAINT cards_pkey PRIMARY KEY (id);


--
-- Name: invoices invoices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoices
    ADD CONSTRAINT invoices_pkey PRIMARY KEY (id);


--
-- Name: orders orders_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.orders
    ADD CONSTRAINT orders_pkey PRIMARY KEY (id);


--
-- Name: payments_helphaver payments_helphaver_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_helphaver
    ADD CONSTRAINT payments_helphaver_pkey PRIMARY KEY (id);


--
-- Name: payments_offline payments_offline_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_offline
    ADD CONSTRAINT payments_offline_pkey PRIMARY KEY (id);


--
-- Name: payments_pelecard payments_pelecard_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_pelecard
    ADD CONSTRAINT payments_pelecard_pkey PRIMARY KEY (id);


--
-- Name: payments payments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_pkey PRIMARY KEY (id);


--
-- Name: specials specials_email_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.specials
    ADD CONSTRAINT specials_email_key UNIQUE (email);


--
-- Name: idx_accounts_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_accounts_deleted_at ON public.accounts USING btree (deleted_at);


--
-- Name: idx_invoices_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoices_deleted_at ON public.invoices USING btree (deleted_at);


--
-- Name: idx_orders_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_orders_deleted_at ON public.orders USING btree (deleted_at);


--
-- Name: idx_payments_deleted_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_payments_deleted_at ON public.payments USING btree (deleted_at);


--
-- Name: payments_offline fk_payment_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_offline
    ADD CONSTRAINT fk_payment_id FOREIGN KEY (payment_id) REFERENCES public.payments(id);


--
-- Name: payments_helphaver fk_payment_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_helphaver
    ADD CONSTRAINT fk_payment_id FOREIGN KEY (payment_id) REFERENCES public.payments(id);


--
-- Name: payments_pelecard fk_payment_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments_pelecard
    ADD CONSTRAINT fk_payment_id FOREIGN KEY (payment_id) REFERENCES public.payments(id);


--
-- PostgreSQL database dump complete
--

