BEGIN;

ALTER TABLE payments_offline
ALTER COLUMN deleted_at SET DEFAULT now();

ALTER TABLE payments_helphaver
ALTER COLUMN deleted_at SET DEFAULT now();

COMMIT;