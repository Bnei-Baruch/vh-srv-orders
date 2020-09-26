package main

import "fmt"

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

//GetAllAccountsWithDuplicates returns array of duplicate accounts
func GetAllAccountsWithDuplicates(filter string) ([]uint, error) {
	//TODO: refactor using ORM functions
	req := `
	-- All accounts who paid more than once
	select distinct "AccountID"
	from orders where "Status" = 'paid' 
	and "Flag" = 'duplicate'
	`

	rows, err := DB.Raw(req).Rows()
	if err != nil {
		return []uint{0}, err

	}

	defer rows.Close()

	var accountsWithDuplicate []uint
	var aid uint
	count := 0

	for rows.Next() {
		rows.Scan(&aid)
		accountsWithDuplicate = append(accountsWithDuplicate, aid)
		count++
	}
	fmt.Println(count)

	return accountsWithDuplicate, nil
}

//GetAccountsWithDuplicatesByMonth returns array of duplicate accounts
func GetAccountsWithDuplicatesByMonth(month string) ([]uint, error) {
	req := `
	-- All accounts who paid more than once
	select distinct "AccountID"
	from orders where "Status" = 'paid' 
	and "Flag" = 'duplicate'
	and date_part('month', "PaymentDate") = ?
	`

	rows, err := DB.Raw(req, month).Rows()
	if err != nil {
		return []uint{0}, err
	}

	defer rows.Close()

	var accountsWithDuplicate []uint
	var aid uint
	count := 0

	for rows.Next() {
		rows.Scan(&aid)
		accountsWithDuplicate = append(accountsWithDuplicate, aid)
		count++
	}
	fmt.Println(count)

	return accountsWithDuplicate, nil
}
