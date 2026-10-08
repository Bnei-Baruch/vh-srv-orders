BEGIN;

DROP INDEX IF EXISTS accounts_userkey_uniq;

INSERT INTO accounts
SELECT (jsonb_populate_record(NULL::accounts, m.loser)).*
FROM account_merges m;

UPDATE orders o SET "AccountID" = mv.loser_id
FROM account_merge_moves mv WHERE mv.tbl = 'orders' AND o.id = mv.row_id;
UPDATE card_details c SET account_id = mv.loser_id
FROM account_merge_moves mv WHERE mv.tbl = 'card_details' AND c.id = mv.row_id;
UPDATE transaction t SET account_id = mv.loser_id
FROM account_merge_moves mv WHERE mv.tbl = 'transaction' AND t.id = mv.row_id;

DROP TABLE account_merge_moves;
DROP TABLE account_merges;

COMMIT;
