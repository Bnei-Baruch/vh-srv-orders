BEGIN;

DROP INDEX IF EXISTS accounts_userkey_uniq;

INSERT INTO accounts
SELECT (jsonb_populate_record(NULL::accounts, m.loser)).*
FROM account_merges m;

-- Only rows still on the account the merge gave them: anything moved since
-- (e.g. by an admin merge into a third account) stays where it was put.
UPDATE orders o SET "AccountID" = mv.loser_id
FROM account_merge_moves mv JOIN account_merges m ON m.loser_id = mv.loser_id
WHERE mv.tbl = 'orders' AND o.id = mv.row_id AND o."AccountID" = m.winner_id;
UPDATE card_details c SET account_id = mv.loser_id
FROM account_merge_moves mv JOIN account_merges m ON m.loser_id = mv.loser_id
WHERE mv.tbl = 'card_details' AND c.id = mv.row_id AND c.account_id = m.winner_id;
UPDATE transaction t SET account_id = mv.loser_id
FROM account_merge_moves mv JOIN account_merges m ON m.loser_id = mv.loser_id
WHERE mv.tbl = 'transaction' AND t.id = mv.row_id AND t.account_id = m.winner_id;

DROP TABLE account_merge_moves;
DROP TABLE account_merges;

COMMIT;
