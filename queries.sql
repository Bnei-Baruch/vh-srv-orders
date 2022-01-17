-- List of order for a given user by email
select a.id, a."Email",
o.created_at, o."Amount", o."Currency",  o.id, o."Status",o."PaymentDate", o."Flag" , o."Type", o.userkey, o.updated_at, o."ProductType"
from accounts as a, orders as o where a."Email" like '%youremail@mail.com%'
and a.id = o."AccountID" order by "PaymentDate" desc 


-- List of payments order by last payment for a given user by email
-- Also display Product Type, and Order Type
select o."ProductType", o."Type", p.* 
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
