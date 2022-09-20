BEGIN;

CREATE TABLE IF NOT EXISTS specials
(
    email                       TEXT NOT NULL,
    category                    TEXT NOT NULL,
    subcategory                 TEXT,
    CONSTRAINT specials_email_key UNIQUE(email)
);

COMMIT;
