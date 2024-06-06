BEGIN;

CREATE TABLE specials (
                            id serial PRIMARY KEY,
                            keycloak_id varchar(85),
                            email varchar(100) not null,
                            start_date timestamp with time zone,
                            end_date timestamp with time zone,
                            created_at timestamp with time zone,
                            category varchar(50) not null,
                            subcategory varchar(50)
);

COMMIT;