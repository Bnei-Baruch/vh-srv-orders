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