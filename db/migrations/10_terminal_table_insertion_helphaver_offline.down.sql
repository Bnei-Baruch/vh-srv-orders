BEGIN;

DELETE FROM terminal
WHERE id IN ('ben_helphaver', 'ben_offline');

COMMIT;