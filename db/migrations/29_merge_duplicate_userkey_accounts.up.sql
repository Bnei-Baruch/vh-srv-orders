BEGIN;

-- Some "UserKey"s are shared by several accounts (524 keys / 1,106 accounts in
-- production, mostly created 2020-2021). GetOrCreateAccount picked the newest
-- and GetAccountIDByKeycloakID whichever row came first, so one person's orders
-- and their pricing/payments could be read from different accounts.
--
-- Each key keeps its newest account — where GetOrCreateAccount has been sending
-- new orders — and every row pointing at an older one is moved to it. The moved
-- rows and the deleted accounts are recorded so the down migration can undo it.

CREATE TABLE account_merges (
    loser_id   INT PRIMARY KEY,
    winner_id  INT NOT NULL,
    loser      JSONB NOT NULL,   -- the deleted accounts row, verbatim
    merged_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE account_merge_moves (
    tbl       TEXT NOT NULL,
    row_id    INT NOT NULL,
    loser_id  INT NOT NULL,
    PRIMARY KEY (tbl, row_id)
);

INSERT INTO account_merges (loser_id, winner_id, loser)
SELECT a.id, w.winner_id, to_jsonb(a)
FROM accounts a
JOIN (SELECT "UserKey", max(id) AS winner_id
      FROM accounts
      WHERE "UserKey" IS NOT NULL
      GROUP BY 1
      HAVING count(*) > 1) w ON w."UserKey" = a."UserKey"
WHERE a.id <> w.winner_id;

INSERT INTO account_merge_moves (tbl, row_id, loser_id)
SELECT 'orders', o.id, o."AccountID" FROM orders o JOIN account_merges m ON m.loser_id = o."AccountID";
INSERT INTO account_merge_moves (tbl, row_id, loser_id)
SELECT 'card_details', c.id, c.account_id FROM card_details c JOIN account_merges m ON m.loser_id = c.account_id;
INSERT INTO account_merge_moves (tbl, row_id, loser_id)
SELECT 'transaction', t.id, t.account_id FROM transaction t JOIN account_merges m ON m.loser_id = t.account_id;

-- Cards move as they are, without deduplicating against the winner's: identical
-- card details on one account are legitimate (see migration 20), and
-- orders.card_details_id keeps pointing at a row that still exists.
UPDATE orders o SET "AccountID" = m.winner_id FROM account_merges m WHERE o."AccountID" = m.loser_id;
UPDATE card_details c SET account_id = m.winner_id FROM account_merges m WHERE c.account_id = m.loser_id;
UPDATE transaction t SET account_id = m.winner_id FROM account_merges m WHERE t.account_id = m.loser_id;

DELETE FROM accounts a USING account_merges m WHERE a.id = m.loser_id;

-- One account per key from here on. NULL keys (accounts created without one)
-- stay unconstrained.
CREATE UNIQUE INDEX accounts_userkey_uniq ON accounts ("UserKey");

COMMIT;
