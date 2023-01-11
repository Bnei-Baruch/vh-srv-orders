BEGIN;

DELETE FROM terminal
WHERE id IN ('ben_dummy_pelecard');

COMMIT;