package main

func countAccounts() int64 {
	var result int64
	DB.Model(&Account{}).Count(&result)
	return result
}
