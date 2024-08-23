BEGIN;
ALTER TABLE card_details ALTER COLUMN gateway_provider DROP NOT NULL;

ALTER TABLE card_details
DROP CONSTRAINT IF EXISTS uq_cc_number_account_id_gateway_key;

DO $$
    BEGIN
        IF NOT EXISTS (
            SELECT 1
            FROM pg_constraint
            WHERE conname = 'uq_cc_number_account_id'
              AND conrelid = 'card_details'::regclass
        ) THEN
            ALTER TABLE card_details
                ADD CONSTRAINT uq_cc_number_account_id UNIQUE (cc_number, account_id);
        END IF;
    END $$;
COMMIT;