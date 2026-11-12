BEGIN;
ALTER TABLE orders ADD COLUMN userkey character varying(85);
UPDATE orders o SET userkey = a."UserKey" FROM accounts a WHERE a.id = o."AccountID";
COMMIT;
