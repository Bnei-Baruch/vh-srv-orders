package main

func countAccounts() int64 {
	var result int64
	DB.Model(&Account{}).Count(&result)
	return result
}

// CreateOrUpdateAccount account
func CreateOrUpdateAccount(a Account) uint {
	var b Account
	DB.Where(&Account{UserKey: a.UserKey, AccountType: a.AccountType}).FirstOrCreate(&b)

	// if ).RecordNotFound() {
	// 	DB.Create(&a)
	// 	return a.ID
	// }
	DB.Model(&b).Updates(a)
	return b.ID
}
