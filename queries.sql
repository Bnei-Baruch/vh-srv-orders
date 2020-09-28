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