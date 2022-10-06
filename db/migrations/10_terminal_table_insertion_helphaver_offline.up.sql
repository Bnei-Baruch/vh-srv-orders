BEGIN;

INSERT INTO terminal
VALUES ('ben_helphaver', 'helphaver', 'none'),
       ('ben_offline', 'offline', 'none')
ON CONFLICT (id) DO NOTHING;

COMMIT;