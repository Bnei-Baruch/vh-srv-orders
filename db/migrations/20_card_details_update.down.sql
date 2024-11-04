BEGIN;

ALTER TABLE card_details
ADD CONSTRAINT uq_cc_number_account_id UNIQUE (cc_number, account_id);

COMMIT;
