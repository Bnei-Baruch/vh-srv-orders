package main

import (
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

// CreateOrUpdateAccount account
func CreateOrUpdateAccount(ctx *gin.Context, a Account) uint {
	var b Account
	reqAccountExist := `
		select id from accounts where "UserKey" = $1
	`
	if err := DB.QueryRow(ctx, reqAccountExist, a.UserKey).Scan(
		&b.ID,
	); err != nil {
		if err == pgx.ErrNoRows {
			var ID uint
			// Add new account if not exist
			if err := DB.QueryRow(ctx, `INSERT INTO broadcast_url (
				FirstName,
				LastName,
				Email,
				Phone,
				Street,
				City,
				State,
				Postcode,
				Country,
				AccountType,
				UserKey)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
				b.FirstName, b.LastName, b.Email, b.Phone, b.Street, b.City, b.State,
				b.Postcode, b.Country, b.AccountType, b.UserKey).Scan(
				&ID,
			); err != nil {
				return 0
			}
			return ID
		} else {
			return 0
		}
	}
	return b.ID
}
