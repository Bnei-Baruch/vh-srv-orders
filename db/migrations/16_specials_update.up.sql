BEGIN;

DROP TABLE IF EXISTS specials;

CREATE TABLE specials (
                            id serial PRIMARY KEY,
                            keycloak_id varchar(85) NOT NULL,
                            start_date timestamp with time zone,
                            end_date timestamp with time zone,
                            created_at timestamp with time zone,
                            category varchar(50) NOT NULL,
                            subcategory varchar(50)
);

COMMIT;