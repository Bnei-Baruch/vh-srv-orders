BEGIN;

-- 00_initial_struct created payments_pelecard with cc_company_cleared and
-- cc_company_issue; 02_pelecard_payment, which names them cc_company_clearer
-- and cc_company_issuer as the code does, is CREATE TABLE IF NOT EXISTS and so
-- changed nothing where 00 ran first. Production has the code's names; a
-- database built from these migrations did not, and any write of the clearer
-- or the issuer to payments_pelecard failed there. Rename only where the old
-- names exist: a no-op on production.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'payments_pelecard' AND column_name = 'cc_company_cleared')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'payments_pelecard' AND column_name = 'cc_company_clearer') THEN
        ALTER TABLE payments_pelecard RENAME COLUMN cc_company_cleared TO cc_company_clearer;
    END IF;
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'payments_pelecard' AND column_name = 'cc_company_issue')
       AND NOT EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'payments_pelecard' AND column_name = 'cc_company_issuer') THEN
        ALTER TABLE payments_pelecard RENAME COLUMN cc_company_issue TO cc_company_issuer;
    END IF;
END $$;

COMMIT;
