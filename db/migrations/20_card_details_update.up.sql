BEGIN;

ALTER TABLE card_details
DROP CONSTRAINT IF EXISTS uq_cc_number_account_id;

COMMIT;
