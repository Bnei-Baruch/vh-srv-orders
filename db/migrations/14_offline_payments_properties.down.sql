BEGIN;

ALTER TABLE payments_offline
    DROP COLUMN IF EXISTS properties;

COMMIT;