BEGIN;

INSERT INTO terminal
VALUES ('ben_dummy_pelecard', 'dummy', 'pelecard')
ON CONFLICT (id) DO NOTHING;

COMMIT;