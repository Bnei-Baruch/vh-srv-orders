BEGIN;
ALTER TABLE card_details ALTER COLUMN gateway_provider DROP NOT NULL;

ALTER TABLE card_details
DROP CONSTRAINT IF EXISTS uq_cc_number_account_id_gateway_key;

ALTER TABLE card_details
ADD CONSTRAINT uq_cc_number_account_id UNIQUE (cc_number, account_id);
COMMIT;