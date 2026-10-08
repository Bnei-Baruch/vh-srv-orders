BEGIN;

-- Absence is NULL, never ''. Each column below was written both ways and read
-- one way, so rows stored with the other spelling were silently mismatched.

-- specials: a row must be reachable by keycloak_id or email. Duplicates per
-- email are intended (several spans per person), so no UNIQUE.
UPDATE specials SET keycloak_id = NULL WHERE keycloak_id = '';
UPDATE specials SET email = NULL WHERE email = '';
ALTER TABLE specials
  ADD CONSTRAINT specials_identifiers_not_empty
  CHECK (keycloak_id <> '' AND email <> '');
ALTER TABLE specials
  ADD CONSTRAINT specials_has_identifier
  CHECK (keycloak_id IS NOT NULL OR email IS NOT NULL);

-- accounts."UserKey": '' matched every other keyless lookup and merged strangers.
UPDATE accounts SET "UserKey" = NULL WHERE "UserKey" = '';
ALTER TABLE accounts
  ADD CONSTRAINT accounts_userkey_not_empty CHECK ("UserKey" <> '');

-- pelecard_token: the "with token" / "without token" filters tested '' only and
-- dropped NULL rows from both.
UPDATE payments SET pelecard_token = NULL WHERE pelecard_token = '';
ALTER TABLE payments
  ADD CONSTRAINT payments_pelecard_token_not_empty CHECK (pelecard_token <> '');
UPDATE payments_pelecard SET pelecard_token = NULL WHERE pelecard_token = '';
ALTER TABLE payments_pelecard
  ADD CONSTRAINT payments_pelecard_pelecard_token_not_empty CHECK (pelecard_token <> '');

COMMIT;
