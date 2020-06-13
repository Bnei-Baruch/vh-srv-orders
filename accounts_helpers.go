package main

func countAccounts() int64 {
	var result int64
	DB.Model(&Account{}).Count(&result)
	return result
}

// CreateOrUpdateAccount account
func CreateOrUpdateAccount(a Account) uint {
	var b Account
	DB.Where("UserKey = ? and AccountType = ?", a.UserKey, a.AccountType).First(&b)
	if b.ID == 0 {
		DB.Create(&a)
		return a.ID
	}
	DB.Model(&b).Updates(a)
	return b.ID
}
