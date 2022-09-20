BEGIN;

UPDATE payments_offline SET deleted_at=null WHERE deleted_at IS NOT NULL;

UPDATE payments_helphaver SET deleted_at=null WHERE deleted_at IS NOT NULL;

COMMIT;