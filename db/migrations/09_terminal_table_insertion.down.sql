BEGIN;

DELETE FROM terminal
WHERE id IN ('ben_regular_pelecard', 'ben_recurring_pelecard');

COMMIT;