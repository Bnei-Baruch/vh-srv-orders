BEGIN;

DROP TABLE IF EXISTS specials;
CREATE TABLE specials (
                            id serial PRIMARY KEY,
                            keycloak_id varchar(85),
                            email varchar(100),
                            start_date timestamp with time zone not null,
                            end_date timestamp with time zone not null,
                            category varchar(50) not null,
                            subcategory varchar(50),
                            created_at timestamp with time zone,
                            updated_at timestamp with time zone
);

COMMIT;