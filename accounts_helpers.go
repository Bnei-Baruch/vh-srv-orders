package main

import (
	"time"

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
			if err := DB.QueryRow(ctx, `INSERT INTO accounts (
				"FirstName",
				"LastName",
				"Email",
				"Phone",
				"Street",
				"City",
				"State",
				"Postcode",
				"Country",
				"AccountType",
				"UserKey",
				created_at,
				updated_at
			)
			VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13) RETURNING id`,
				a.FirstName, a.LastName, a.Email, a.Phone, a.Street, a.City, a.State,
				a.Postcode, a.Country, a.AccountType, a.UserKey, time.Now(), time.Now()).Scan(
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
