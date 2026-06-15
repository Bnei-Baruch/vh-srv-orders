BEGIN;

DROP INDEX IF EXISTS idx_hh_grants_keycloak_id;
DROP TABLE IF EXISTS hh_grants;
DROP INDEX IF EXISTS idx_hh_requests_keycloak_id;
DROP TABLE IF EXISTS hh_requests;

COMMIT;
