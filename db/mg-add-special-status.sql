BEGIN;
create table specials(
       email varchar(50) not null unique,
       category varchar(50) not null,
       subcategory varchar(50)
);
COMMIT;


-- \copy specials(email, category, subcategory) from './specials.csv' delimiter ',' csv header;
