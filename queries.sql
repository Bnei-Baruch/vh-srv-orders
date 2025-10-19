-- List of order for a given user by email
select a.id, a."Email",
o.created_at, o."Amount", o."Currency",  o.id, o."Status",o."PaymentDate", o."Flag" , o."Type", o.userkey, o.updated_at, o."ProductType"
from accounts as a, orders as o where a."Email" like '%youremail@mail.com%'
and a.id = o."AccountID" order by "PaymentDate" desc 


-- List of payments order by last payment for a given user by email
-- Also display Product Type, and Order Type
select o."ProductType", o."Type", p.created_at,  p."Amount", p."PaymentType",  p."OrderID", p."ParamX", p."PaymentStatus", p."CCNumber", p."CCExpDate"
from payments as p, orders as o  
where p."OrderID" in (select o.id from accounts as a, orders as o where a."Email" like '%youremail@mail.com%'
and a.id = o."AccountID" ) 
and p."OrderID" = o.id 
order by p.created_at desc 



-- List of payments order by last payment for a given user by email
select * from payments 
where "OrderID" in (select o.id from accounts as a, orders as o where a."Email" like '%youremail@mail.com%'
and a.id = o."AccountID" ) order by created_at desc 



-- List of accounts who paid more than once
select mistakes."AccountID"
from (
  select "AccountID", count(*) as "duplicate" 
  from orders where "Status" = 'paid' 
  group by "AccountID" ORDER BY "duplicate" DESC
) as mistakes 
where "duplicate" > 1 


-- Total who paid more than once
select count(*)
from (
  select "AccountID", count(*) as "duplicate" 
  from orders where "Status" = 'paid' 
  group by "AccountID" ORDER BY "duplicate" DESC
) as mistakes 
where "duplicate" > 1 


-- All accounts who paid more than once
select "AccountID", count(*) as "duplicate" 
from orders where "Status" = 'paid'
group by "AccountID" ORDER BY "duplicate" DESC


-- All accounts who paid more than once for a given month
select "AccountID", count(*) as "duplicate" 
from orders where "Status" = 'paid' and date_part('month', "PaymentDate") = 6
group by "AccountID" ORDER BY "duplicate" DESC

/* select "AccountID", count(*) as "duplicate" 
from orders where "Status" = 'paid' and date_part('month', "PaymentDate") = 8
group by "AccountID" 
having count(*) > 1
ORDER BY "duplicate" DESC */

update orders set "Flag" = 'duplicate'
where "AccountID" in (select "AccountID"
from orders where "Status" = 'paid' and date_part('month', "PaymentDate") = 8
group by "AccountID" 
having count(*) > 1) and date_part('month', "PaymentDate") = 8

-- All Orders from June
Select count(*)
from orders
Where "Status" = 'paid' and date_part('month', "PaymentDate") = 6


-- All failed transaction
select a."FirstName", a."LastName", p."ParamX", p."CCNumber", p."CCExpDate"
from accounts as a, payments as p, orders as o 
where p."PaymentStatus" = 'failed'
and p."OrderID" = o.id
and o."AccountID" = a.id 


-- Orders from Account by Email
select * from orders
where "AccountID" in (select id from accounts as a where a."Email" = 'rakelaisra66@gmail.com' )



UPDATE orders SET userkey =  FROM accounts WHERE  orders.AccountID = account.id

-- Mails for order of a certain sum
select a."Email"  
from orders as o, accounts as a  
where o."Status"= 'paid' and o."Amount"= '100' and o."Currency" = 'USD'
and o."AccountID" = a.id


-- Blabla query subscriptions stuff for frontend
select 
o.id, 
o.created_at, 
o."Amount",
o."Currency",
o."PaymentDate", 
o."Status",
o."Flag", 
o."Note",
a."FirstName", a."LastName",
a."Email", a."Country", 
o."OrderLanguage" 
from orders as o, accounts as a 
where o."AccountID" = a.id
and o."Status" <> 'pending'



-- Relevant info
select a.id, a."FirstName", a."LastName",
o."Amount", o."Currency", o."PaymentDate", o."Status", o."Flag"
from accounts as a, 
orders as o
where a."Email" = '60emilia@gmail.com'
and a.id = o."AccountID"



update orders set "Flag"='' where id='2204'
update orders set "Amount"='10', "Currency"='NIS' where id='3325'


Select a."Email", o.id, o."Amount", o."Currency", o."PaymentDate"
from accounts as a, orders as o 
Where "Flag" = 'duplicate'
and a.id = o."AccountID"
and date_part('month', "PaymentDate") = 6


	-- All accounts who paid more than once
	select distinct "AccountID"
	from orders where "Status" = 'paid' 
	and "Flag" = 'duplicate'
	and date_part('month', "PaymentDate") = ?

----

-- select distinct("Email"), "FirstName", "LastName" from accounts where
-- id in (select "AccountID" from orders
-- where "Status" = 'cancelled')
-- and id not in (select "AccountID" from orders where
-- "Status" = 'paid')

-- select sum(p."Amount"), count(distinct o.userkey) from payments p, orders o
-- where
--     p."OrderID" = o.id and
--     p."updated_at" > '2021-02-01 00:00:00' and p.updated_at < '2021-03-01 06:00:00' and
--     o."Currency" = 'EUR' and
--     p."PaymentStatus" = 'success'and
--     o."ProductType" = 'globalmembership'


-- select * from orders
-- where "Status" = 'march'

-- select sum(p."Amount"), count(distinct o.userkey) from payments p, orders o
-- where
--     p."OrderID" = o.id and
--     p."updated_at" > '2021-03-01 06:05:00' and p.updated_at < '2021-03-31 23:58:00' and
--     o."Currency" = 'EUR' and
--     p."PaymentStatus" = 'success'and
--     o."ProductType" = 'globalmembership'


-- select o."Currency", count(o.ID), sum(CAST(o."Amount" as int))
-- from orders as o where o."Status" = 'paid'
-- and (date_part('month', "PaymentDate") = 02)
-- group by o."Currency"


-- update orders set "Flag" = 'paused' where orders.id in
-- (select "OrderID" from payments
-- where  date_trunc('day',payments.created_at) = '2021-01-25%'
-- and payments.success = '1')
-- and "Flag" = 'torenew'

-- select * from orders where id in
-- (select "OrderID" from payments
-- where  date_trunc('day',payments.created_at) = '2021-01-25%'
-- and payments.success = '1')
-- and "Flag" = 'torenew'

-- select * from orders
-- where "Flag" = 'renewed'

-- select * from orders
-- -- where "Flag" = 'torenew'
-- where date_part('month',"PaymentDate") = 3
-- order by "PaymentDate" desc

-- select count(*) from payments
-- where  date_trunc('day',payments.created_at) = '2021-01-25%'
-- and payments.success = '1'


-- select a.id,
-- a."FirstName", a."LastName", a."Email",
-- o.created_at, o."Amount", o."Currency", o."PaymentDate", o."Status", o."Flag", o.id, o."Type", o.updated_at
-- from accounts as a,
-- orders as o
-- where a."Email" like '%adidaus1@gmail.com%'
-- --Where a.id = '3220'
-- and a.id = o."AccountID"
-- order by "PaymentDate" desc


-- select * from orders
-- where "Status" = 'paid'
-- and date_part('month', "PaymentDate") = 4
-- and userkey in (select userkey from orders
-- where "Flag" = 'torenew'
-- and "Status" = 'paid'
-- )


-- select * from payments
-- where "OrderID" in (select id from orders
-- where "Status" = 'paid'
-- and date_part('month', "created_at") = 2
-- limit 10 ) limit 10


-- select accounts."Email", accounts."FirstName", accounts."LastName"
-- from orders, payments, accounts
-- where payments.id = '28314'
-- and payments."OrderID" = orders.id
-- and  orders."AccountID" = accounts.id

-- update orders set "Currency" = 'EUR' where id='5522'
-- update orders set "Amount" = '9' where id='12913'

-- update orders set "Status" = 'cancelled' where id in (
-- select orders.id from orders, accounts
-- where accounts."Email" like 'sfil4497@gmail.com'
-- and accounts.id = orders."AccountID")


  -- select * from payments
  -- where "OrderID" = 3067 -- in (select id from orders where "AccountID" = 1629 )
  -- and "PaymentStatus" = 'success'
  -- order by updated_at desc





-- select o."Amount", o."Currency", o."PaymentDate", o."Status", o."Flag", o.id, o."Type"
-- from orders as o
-- where userkey='639646b3-89ac-49f8-8949-8215c54cf418'
-- order by "PaymentDate" desc


-- select * from payments where "OrderID" in  (select  o.id from orders as o where userkey='639646b3-89ac-49f8-8949-8215c54cf418')



-- select count(*) from orders where "Type"='regular' and "Status"='paid'

-- select count(*) from orders where created_at > '01/15/2021' and "Type"='recurring'and "Status"='paid' and userkey not in (select userkey from orders where created_at < '01/15/2021')

-- select userkey, count(userkey) as qt from orders where "Flag" = 'torenew' group by userkey order by qt asc
-- select count(*) from orders where "Flag" = 'renewed'
-- where a."Email" like 'tabanova.0101@gmail.com'

-- count recurring orders by country
WITH updates as (select distinct on (o.userkey) o.id, o.userkey, "PaymentDate", "Amount", "Currency", a."Country"
                 from orders o
                          inner join accounts a on o."AccountID" = a.id
                 where ("Status" = 'paid' or "Status" = 'success' or "Status" = 'nosuccess')
                   and "ProductType" = 'globalmembership'
                   and "Type" = 'recurring'
                 order by userkey, "PaymentDate" desc)
select "Country", count(*)
from updates
group by "Country"
order by "Country";

---
select "Type", count(*) from orders where "ProductType" = 'globalmembership' group by "Type";
select "Type", count(*) from orders where "ProductType" = 'globalmembership' and "Status" = 'nosuccess' group by "Type";

select "Status", count(*) from orders where "ProductType" = 'globalmembership' group by "Status" order by count(*) desc;

select distinct on (userkey)
    id, "AccountID", created_at, updated_at, "RecuringFreq", "Amount", "Currency", "PaymentDate", "Flag", quantity,
    amount_item, starting_date, DATE_PART('day', "PaymentDate" - starting_date)
from orders
where "Type" = 'regular'
  and "ProductType" = 'globalmembership'
  and "Status" = 'paid'
order by userkey, "PaymentDate" desc;

copy
    (with emails as (select email
                     from specials
                     union all
                     select a."Email" as email
                     from accounts a
                              inner join orders o on a.id = o."AccountID"
                     where "ProductType" = 'globalmembership')
     select distinct email
     from emails
     order by email)
    to '/backup/accounts_emails.csv' csv;

with last_order as (select distinct on (userkey) *
                    from orders
                    where "ProductType" = 'globalmembership'
                    order by userkey, "PaymentDate" desc)
select o.id, o."Type", o."Status", o."Flag", s.*
from last_order o
         inner join accounts a on o."AccountID" = a.id
         inner join specials s on a."Email" = s.email
where o."Status" = 'paid' or o."Status" = 'success' or o."Status" = 'nosuccess';

copy (
    with last_order as (select distinct on (userkey) *
                        from orders
                        where "ProductType" = 'globalmembership'
                          and "Status" in ('paid', 'success', 'nosuccess', 'cancelled')
                        order by userkey, "PaymentDate" desc)
    select distinct a."Email"
    from last_order o
             inner join accounts a on o."AccountID" = a.id
    where o."Status" = 'cancelled'
    order by a."Email")
    to '/backup/orders_cancelled_emails.csv' csv;

with last_order as (select distinct on (userkey) *
                    from orders
                    where "ProductType" = 'globalmembership'
                      and "Status" in ('paid', 'success', 'nosuccess', 'cancelled')
                    order by userkey, "PaymentDate" desc)
select "Flag", count(*)
from last_order o
group by "Flag"
order by "Flag";


-- data standardization

-- currency
select "Currency", count(*) from orders group by "Currency" order by "Currency";

update orders set "Currency"= 'USD' where "Currency" in ('dollar', 'usd');
update orders set "Currency"= 'EUR' where "Currency" = 'EURO';
update orders set "Currency"= 'USD' where id in (10523,10538,10534,10539,10537,10525,10522,10526,10524);

delete from orders where id=18300;

-- status
select "Status", count(*) from orders group by "Status" order by "Status";

update orders set "Status"= 'paid' where "Status" = 'april';
update orders set "Status"= 'paid' where "Status" = 'double12';
update orders set "Status"= 'paid' where "Status" = 'group';
update orders set "Status"= 'paid' where "Status" = 'manualtransfer';
update orders set "Status"= 'paid' where "Status" = 'test';
update orders set "Status"= 'cancelled' where "Status" = 'canceled';
update orders set "Status"= 'cancelled' where "Status" = 'nulled';
update orders set "Status"= 'cancelled' where "Status" = 'paused';
update orders set "Status"= 'cancelled' where "Status" = 'removed';
update orders set "Status"= 'cancelled' where "Status" = 'stopped';
update orders set "Status"= 'pending' where "Status" = 'failed';


select o."Status", p."PaymentType", p."PaymentStatus", count(o.id), count(p.id)
from orders o
         left join payments p on o.id = p."OrderID"
where o."Status" in ('april', 'double12', 'failed', 'group', 'manualtransfer', 'refunded', 'test')
group by o."Status", p."PaymentType", p."PaymentStatus"
order by o."Status", p."PaymentType", p."PaymentStatus";

select o."Status", p."PaymentType", p."PaymentStatus", count(o.id), count(p.id)
from orders o
         left join payments p on o.id = p."OrderID"
group by o."Status", p."PaymentType", p."PaymentStatus"
order by o."Status", p."PaymentType", p."PaymentStatus";


-- OrderLanguage
select "OrderLanguage", count(*) from orders group by "OrderLanguage" order by "OrderLanguage";

update orders set "OrderLanguage"= 'EN' where "OrderLanguage" in ('en', '');
update orders set "OrderLanguage"= 'ES' where "OrderLanguage" = 'SP';


-- payment "PaymentStatus"
select "PaymentStatus", count(*) from payments group by "PaymentStatus" order by "PaymentStatus";

update payments set "PaymentStatus"= 'success' where "PaymentStatus" = 'sucess';
update payments set "PaymentStatus"= 'success' where "PaymentStatus" = 'paid';

-- payment "DebitCurrency"
--TODO (payment currency)
update payments set "DebitCurrency"= null where "DebitCurrency" = '';

-- specials cleanup

select count(*) from specials;
select count(*) from specials where subcategory <> 'rav';
delete from specials where subcategory <> 'rav';


--- group status
select "Status", count(*)
from orders
where id in (9996,3598,3052,7469,11927,10658,14906,8075,3734,5198,3288,1706,3625,8957,3254,10215,5182,6834,9455,5070,1323,6154,9662,11136,9195,4061,2060,1542,12787,2723,98,10945,9443,10211,10892,16169,7821,6469,6176,10456,14984,5195,16621,8077,30,10059,5227,10314,6944,14920,8385,10268,12398,15815,1051,14956,18050,2759,11923,17798,10354,10098,6396,555,11924,2560,14781,7093,4450,1698,1640,1476,616,683,9734,3066,16049,9521,14990,4286,3565,7539,9078,11993,15335,6263,8984,7490,16480,7636,10532,8779,4636,7134,12278,11289,18513,2522,4855,671,14752,8421,15429,2180,2125,7151,17926,2573,2666,14487,11744,16650,15294,11813,116,13153,16046,3058,5317,5200,9368,5944,14218,17819,8088,6814,3717,13148,6635,3468,10003,2530,5736,8906,8362,9109,3133,18014,4467,15932,17846,2097,3990,1840,6918,9733,18095,9967,4218,10815,1857,5933,10294,13409)
group by "Status";

SELECT rank_filter.*
FROM (SELECT a."Email",
             a."Country",
             o.id              o_id,
             o.created_at      o_created_at,
             o."Type"          o_type,
             o."Amount"        o_amount,
             o."Currency"      o_currency,
             o."Status"        o_status,
             o."PaymentDate"   o_payment_date,
             p.id              p_id,
             p.created_at      p_created_at,
             p."PaymentType"   p_payment_type,
             p."Amount"        p_amount,
             p."PaymentStatus" p_status,
             row_number() OVER (
                 PARTITION BY o.id
                 ORDER BY p.created_at DESC
                 )             row_num
      FROM orders o
               inner join payments p on o.id = p."OrderID"
               inner join accounts a on o."AccountID" = a.id
      WHERE o.id in
            (9996, 3598, 3052, 7469, 11927, 10658, 14906, 8075, 3734, 5198, 3288, 1706, 3625, 8957, 3254, 10215, 5182,
             6834, 9455, 5070, 1323, 6154, 9662, 11136, 9195, 4061, 2060, 1542, 12787, 2723, 98, 10945, 9443, 10211,
             10892, 16169, 7821, 6469, 6176, 10456, 14984, 5195, 16621, 8077, 30, 10059, 5227, 10314, 6944, 14920, 8385,
             10268, 12398, 15815, 1051, 14956, 18050, 2759, 11923, 17798, 10354, 10098, 6396, 555, 11924, 2560, 14781,
             7093, 4450, 1698, 1640, 1476, 616, 683, 9734, 3066, 16049, 9521, 14990, 4286, 3565, 7539, 9078, 11993,
             15335, 6263, 8984, 7490, 16480, 7636, 10532, 8779, 4636, 7134, 12278, 11289, 18513, 2522, 4855, 671, 14752,
             8421, 15429, 2180, 2125, 7151, 17926, 2573, 2666, 14487, 11744, 16650, 15294, 11813, 116, 13153, 16046,
             3058, 5317, 5200, 9368, 5944, 14218, 17819, 8088, 6814, 3717, 13148, 6635, 3468, 10003, 2530, 5736, 8906,
             8362, 9109, 3133, 18014, 4467, 15932, 17846, 2097, 3990, 1840, 6918, 9733, 18095, 9967, 4218, 10815, 1857,
             5933, 10294, 13409)) rank_filter
WHERE row_num = 1 and p_created_at > '01-12-2023' order by p_status;


select rank_filter.*
from (select o."AccountID",
             o.id,
             o.created_at    o_created_at,
             o."Type"        o_type,
             o."Amount"      o_amount,
             o."Currency"    o_currency,
             o."Status"      o_status,
             o."PaymentDate" o_payment_date,
             row_number() OVER (
                 PARTITION BY o."AccountID"
                 ORDER BY o.created_at DESC
                 )           row_num
      from orders o
      where o."ProductType" = 'globalmembership'
        and (o."Status" = 'paid' or o."Status" = 'nosuccess')
        and o."AccountID" in
            (SELECT distinct o."AccountID"
             FROM orders o
             WHERE o.id in
                   (9996, 3598, 3052, 7469, 11927, 10658, 14906, 8075, 3734, 5198, 3288, 1706, 3625, 8957, 3254, 10215,
                    5182,
                    6834, 9455, 5070, 1323, 6154, 9662, 11136, 9195, 4061, 2060, 1542, 12787, 2723, 98, 10945, 9443,
                    10211,
                    10892, 16169, 7821, 6469, 6176, 10456, 14984, 5195, 16621, 8077, 30, 10059, 5227, 10314, 6944,
                    14920, 8385,
                    10268, 12398, 15815, 1051, 14956, 18050, 2759, 11923, 17798, 10354, 10098, 6396, 555, 11924, 2560,
                    14781,
                    7093, 4450, 1698, 1640, 1476, 616, 683, 9734, 3066, 16049, 9521, 14990, 4286, 3565, 7539, 9078,
                    11993,
                    15335, 6263, 8984, 7490, 16480, 7636, 10532, 8779, 4636, 7134, 12278, 11289, 18513, 2522, 4855, 671,
                    14752,
                    8421, 15429, 2180, 2125, 7151, 17926, 2573, 2666, 14487, 11744, 16650, 15294, 11813, 116, 13153,
                    16046,
                    3058, 5317, 5200, 9368, 5944, 14218, 17819, 8088, 6814, 3717, 13148, 6635, 3468, 10003, 2530, 5736,
                    8906,
                    8362, 9109, 3133, 18014, 4467, 15932, 17846, 2097, 3990, 1840, 6918, 9733, 18095, 9967, 4218, 10815,
                    1857,
                    5933, 10294, 13409))) rank_filter
WHERE row_num < 4;

-- double12
SELECT rank_filter.*
FROM (SELECT a."Email",
             a."Country",
             o.id              o_id,
             o.created_at      o_created_at,
             o."Type"          o_type,
             o."Amount"        o_amount,
             o."Currency"      o_currency,
             o."Status"        o_status,
             o."PaymentDate"   o_payment_date,
             p.id              p_id,
             p.created_at      p_created_at,
             p."PaymentType"   p_payment_type,
             p."Amount"        p_amount,
             p."PaymentStatus" p_status,
             row_number() OVER (
                 PARTITION BY o.id
                 ORDER BY p.created_at DESC
                 )             row_num
      FROM orders o
               inner join payments p on o.id = p."OrderID"
               inner join accounts a on o."AccountID" = a.id
      WHERE o.id in
            (8258, 8982, 8027, 8254, 7479, 8873, 8760, 8217, 7438, 8989, 8675, 8996, 8844, 8109, 7973, 9006, 8846, 8960,
             8603, 5651, 8697, 7805, 8545, 4885, 7049, 8600, 9019, 6654, 6552, 1600, 8586, 8983, 7141, 8788, 8588, 7832,
             8001, 8926, 8634, 7997, 7994, 8647, 8755, 8346, 8618, 8465, 8116, 8676, 8594, 8939, 8746, 8953, 7965, 6386,
             9017, 2482, 5496, 5901, 8942, 3516, 7988, 8985, 8905, 5541, 8975, 8665, 8925, 8546, 8108, 8107, 8699, 8436,
             9002, 8771, 802, 8002, 8087, 974)) rank_filter
WHERE row_num < 4;

-- april
SELECT rank_filter.*
FROM (SELECT a."Email",
             a."Country",
             o.id              o_id,
             o.created_at      o_created_at,
             o."Type"          o_type,
             o."Amount"        o_amount,
             o."Currency"      o_currency,
             o."Status"        o_status,
             o."PaymentDate"   o_payment_date,
             p.id              p_id,
             p.created_at      p_created_at,
             p."PaymentType"   p_payment_type,
             p."Amount"        p_amount,
             p."PaymentStatus" p_status,
             row_number() OVER (
                 PARTITION BY o.id
                 ORDER BY p.created_at DESC
                 )             row_num
      FROM orders o
               inner join payments p on o.id = p."OrderID"
               inner join accounts a on o."AccountID" = a.id
      WHERE o.id in
            (14481)) rank_filter
WHERE row_num = 1 and p_created_at > '01-12-2023' order by p_status;

-- manualtransfer

SELECT rank_filter.*
FROM (SELECT a."Email",
             a."Country",
             o.id              o_id,
             o.created_at      o_created_at,
             o."Type"          o_type,
             o."Amount"        o_amount,
             o."Currency"      o_currency,
             o."Status"        o_status,
             o."PaymentDate"   o_payment_date,
             p.id              p_id,
             p.created_at      p_created_at,
             p."PaymentType"   p_payment_type,
             p."Amount"        p_amount,
             p."PaymentStatus" p_status,
             row_number() OVER (
                 PARTITION BY o.id
                 ORDER BY p.created_at DESC
                 )             row_num
      FROM orders o
               inner join payments p on o.id = p."OrderID"
               inner join accounts a on o."AccountID" = a.id
      WHERE o.id in
            (5759,5007,2566,3210,3215,1866,4610,438,2565,1112,530,342)) rank_filter
WHERE row_num = 1 and p_created_at > '01-12-2023' order by p_status;


-- payment currency

select *
from (select p.id,
             p."OrderID",
             p."Amount",
             p."DebitCurrency",
             o."Currency" as o_currency,
             case
                 when (p."DebitCurrency" = '1') then 'NIS'
                 when (p."DebitCurrency" = '2') then 'USD'
                 when (p."DebitCurrency" = '978') then 'EUR'
                 when (p."DebitCurrency" = '0') then 'EUR'
                 when (p."DebitCurrency" is null or p."DebitCurrency" = '') then o."Currency"
                 end      as currency
      from payments p
               inner join orders o on p."OrderID" = o.id) as tmp
where currency <> o_currency;

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



-- IL cards paying not in NIS
select * from payments_pelecard 
where debit_total='1'
order by created_at desc limit 50;

select payment_id, cardhebrew_name, cc_abroad_card, debit_currency, debit_total 
from payments_pelecard where payment_status='success' order by created_at desc limit 50;

select j_param, count(*) from payments_pelecard where payment_status='success' group by j_param;
select success, count(*) from payments_pelecard group by success;
select payment_status, count(*) from payments_pelecard where success ='1' group by payment_status;
select count(*) from payments_pelecard where payment_status='success' and success !='1';

select payment_id, cardhebrew_name, cc_abroad_card, debit_currency, debit_total 
from payments_pelecard pp where pp.success = '1' ;


select "PaymentType", count(*) from payments group by "PaymentType";


with last_chargeable_order as (
  select distinct on (userkey) *
  from orders
  where "ProductType" = 'globalmembership' and "Type" = 'recurring' and "Status" in ('paid', 'nosuccess')
  order by userkey, "PaymentDate" desc
),
last_payment as (
  select distinct on ("OrderID") * from payments order by "OrderID", updated_at desc, created_at desc
)
select
  a.id,
  a."Email" as email,
  a."UserKey" as keycloak_id,
  a."Country" as country,
  o.id as order_id,
  o.created_at as o_created_at,
  lp.id as payment_id,
  lp.created_at as p_created_at,
  lp."CardHebrewName" as p_cc_heb_name, 
  lp."CCAbroadCard" as p_cc_abroad_card, 
  lp."Currency" as p_cc_currency, 
  lp."Amount" as p_cc_amount
from
  accounts a
  inner join last_chargeable_order lco on a.id = lco."AccountID"
  inner join orders o on lco.id = o.id
  inner join last_payment lp on lp."OrderID" = o.id and lp."PaymentType"='pelecard' and lp.success ='1' and lp."Currency" != 'NIS'
  where a."Country" in ('Israel', 'IL')
  ;
  
  
  
with last_chargeable_order as (
  select distinct on (userkey) *
  from orders
  where
    "ProductType" = 'globalmembership' and "Type" = 'recurring' and "Status" in ('paid', 'nosuccess')
  order by userkey, "PaymentDate" desc
)
select
  a.id,
  a."Email" as email,
  a."UserKey" as keycloak_id,
  a."Country" as country,
  o.id as order_id,
  o.created_at as o_created_at
from
  accounts a
  inner join last_chargeable_order lco on a.id = lco."AccountID"
  inner join orders o on lco.id = o.id
  ;