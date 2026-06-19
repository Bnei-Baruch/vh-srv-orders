-- One-off migration: copy active Help Haver grants from the profiles DB into orders.
-- Every orders grant must be linked to a request, so each active profiles grant
-- becomes an approved hh_request + hh_grant pair.
--
-- Step 1: run this SELECT on the PROFILES database. It prints one statement
--         per active grant (status APPROVED, not cancelled, not yet expired).
-- Step 2: review the output, then paste it into psql on the ORDERS database
--         (after migration 26_help_haver_v2 has been applied).
--
-- Notes:
-- - Old profiles grants have no discount_pct, so they import as 100% (free) —
--   matching the old behavior where an approved HH request made membership free.
-- - Grants for members in v1-pricing countries are harmless: v1 never reads hh_grants.
-- - The request type is carried over when present; legacy grants fall back to 'hh-other'.

SELECT format(
  'WITH r AS (INSERT INTO hh_requests (keycloak_id, type, requested_pct, months, note, status, created_at) '
  || 'VALUES (%L, %L, %s, %s, %L, ''APPROVED'', %L) RETURNING id) '
  || 'INSERT INTO hh_grants (request_id, keycloak_id, type, discount_pct, start_date, end_date, note) '
  || 'SELECT id, %L, %L, %s, %L, %L, %L FROM r;',
  r.keycloak_id,
  CASE WHEN r.type IN ('hh-hayal', 'hh-gimlaj', 'hh-other') THEN r.type ELSE 'hh-other' END,
  COALESCE((g.properties->>'discount_pct')::int, 100),
  LEAST((g.properties->>'months')::int, 12),
  'migrated from profiles request #' || r.id,
  g.created_at,
  r.keycloak_id,
  CASE WHEN r.type IN ('hh-hayal', 'hh-gimlaj', 'hh-other') THEN r.type ELSE 'hh-other' END,
  COALESCE((g.properties->>'discount_pct')::int, 100),
  g.created_at,
  g.created_at + ((g.properties->>'months')::int || ' months')::interval,
  'migrated from profiles grant #' || g.id
) AS migrate_stmt
FROM "grant" g
JOIN request r ON r.id = g.request_id
WHERE g.cancelled_at IS NULL
  AND (g.properties->>'months') IS NOT NULL
  AND g.created_at + ((g.properties->>'months')::int || ' months')::interval > now()
ORDER BY g.created_at;
