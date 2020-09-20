# Roadmap

Payments
- [ ] Clean duplicate subscriptions
- [ ] Change currency


- [ ] Admin end points :
  /subscriptions (all with pagination / by ID - return accountID)
  /payments 
  
## Clean duplicate subscriptions

Get all account with duplicates - return array of id [done]
for each account id, get all orders ID paid [done]
For each id:
  - current month (ex: 09)
  - start month of user (ex: 07)
    (activeMonths = currentMonth - startMonth) (9-7 = 2)
    if ordersNumbers > activeMonth
    hasDuplicate
  
if hasDuplicate
  case 1) nbuplicates < activeMonth
    cancel all beside lastOne
  case 2) nbuplicates > activeMonth
    mark all as duplicate and wait for manuel fix


 ## Check active orders not renewed 
 get all active order not renewed last month