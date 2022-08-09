BEGIN;

ALTER TABLE payments_offline
ALTER COLUMN deleted_at DROP DEFAULT;

ALTER TABLE payments_helphaver
ALTER COLUMN deleted_at DROP DEFAULT;

COMMIT;