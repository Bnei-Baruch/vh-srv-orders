BEGIN;

UPDATE orders
SET "SKU" = '40037' 
WHERE "ProductType" = 'globalmembership';

UPDATE orders
SET "SKU" = '40033' 
WHERE "ProductType" != 'globalmembership';

COMMIT;