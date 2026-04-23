BEGIN;
ALTER TABLE payments DROP COLUMN IF EXISTS pricing_evaluation;
ALTER TABLE payments DROP COLUMN IF EXISTS pricing_version;
COMMIT;
