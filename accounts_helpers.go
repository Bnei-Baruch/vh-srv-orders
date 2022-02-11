package main

import "fmt"

// CreateOrUpdateAccount account
func CreateOrUpdateAccount(a Account) uint {
	var b Account
	reqAccountExist := `
		select * from accounts where "UserKey" = ? limit 1
	`
	DB.Raw(reqAccountExist, a.UserKey).Scan(&b)

	if b.ID == 0 {
		fmt.Println("CREATE NEW ACCOUNT")
		DB.Create(&a)
		return a.ID
	}
	return b.ID
}
