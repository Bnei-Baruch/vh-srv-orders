BEGIN;

ALTER TABLE payments_pelecard DROP COLUMN terminal;

COMMIT;
