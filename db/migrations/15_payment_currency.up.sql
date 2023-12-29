BEGIN;

ALTER TABLE payments
    ADD COLUMN IF NOT EXISTS "Currency" varchar(3) DEFAULT null;

with updates as (select p.id,
                        case
                            when (p."DebitCurrency" = '1') then 'NIS'
                            when (p."DebitCurrency" = '2') then 'USD'
                            when (p."DebitCurrency" = '978') then 'EUR'
                            when (p."DebitCurrency" = '0') then 'EUR'
                            when (p."DebitCurrency" is null or p."DebitCurrency" = '') then o."Currency"
                            end as currency
                 from payments p
                          inner join orders o on p."OrderID" = o.id)
update payments
set "Currency" = updates.currency
from updates
where updates.id = payments.id;


COMMIT;