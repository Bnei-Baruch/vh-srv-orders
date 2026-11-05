BEGIN;

-- orders.userkey was a copy of accounts."UserKey", refreshed only by the
-- monthly billing run: NULL for every order created since, and for orders with
-- no account. Readers now go through "AccountID", which since migration 29 is
-- one account per key.
ALTER TABLE orders DROP COLUMN userkey;

COMMIT;
