BEGIN;

ALTER TABLE payments_pelecard ADD COLUMN terminal VARCHAR(10);

COMMIT;
