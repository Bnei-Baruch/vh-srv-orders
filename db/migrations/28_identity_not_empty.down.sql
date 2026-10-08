BEGIN;
ALTER TABLE payments_pelecard DROP CONSTRAINT IF EXISTS payments_pelecard_pelecard_token_not_empty;
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_pelecard_token_not_empty;
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_userkey_not_empty;
ALTER TABLE specials DROP CONSTRAINT IF EXISTS specials_has_identifier;
ALTER TABLE specials DROP CONSTRAINT IF EXISTS specials_identifiers_not_empty;
COMMIT;
