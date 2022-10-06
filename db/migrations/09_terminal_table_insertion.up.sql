BEGIN;

INSERT INTO terminal
VALUES ('ben_regular_pelecard', '5776492014', 'pelecard'),
       ('ben_recurring_pelecard', '2814722016', 'pelecard')
ON CONFLICT (id) DO NOTHING;

COMMIT;