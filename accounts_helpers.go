package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

// CreateOrUpdateAccount account
func CreateOrUpdateAccount(ctx *gin.Context, a Account) uint {
	var b Account
	reqAccountExist := `
		select id from accounts where "UserKey" = $1 ORDER BY id DESC LIMIT 1
	`
	if err := DB.QueryRow(ctx, reqAccountExist, a.UserKey).Scan(
		&b.ID,
	); err != nil {
		if err == pgx.ErrNoRows {

			createString, numString, createQueryArgs := prepareAccountCreateQuery(a)

			var ID uint
			// Add new account if not exist
			if len(createQueryArgs) != 0 {
				if err := DB.QueryRow(ctx, fmt.Sprintf(`INSERT INTO accounts (%s) VALUES (%s) RETURNING id`, createString, numString),
					createQueryArgs...).Scan(
					&ID,
				); err != nil {
					return 0
				}
				return ID
			} else {
				return 0
			}

		} else {
			return 0
		}
	}
	return *b.ID
}

func prepareAccountCreateQuery(req Account) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.FirstName != nil {
		createStrings = append(createStrings, `"FirstName"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.FirstName)
	}
	if req.LastName != nil {
		createStrings = append(createStrings, `"LastName"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.LastName)
	}
	if req.Email != nil {
		createStrings = append(createStrings, `"Email"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Email)
	}
	if req.Phone != nil {
		createStrings = append(createStrings, `"Phone"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Phone)
	}
	if req.Street != nil {
		createStrings = append(createStrings, `"Street"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Street)
	}
	if req.City != nil {
		createStrings = append(createStrings, `"City"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.City)
	}
	if req.State != nil {
		createStrings = append(createStrings, `"State"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.State)
	}
	if req.Postcode != nil {
		createStrings = append(createStrings, `"Postcode"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Postcode)
	}
	if req.Country != nil {
		createStrings = append(createStrings, `"Country"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.Country)
	}
	if req.AccountType != nil {
		createStrings = append(createStrings, `"AccountType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.AccountType)
	}
	if req.PaymentToken != nil {
		createStrings = append(createStrings, `"PaymentToken"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PaymentToken)
	}
	if req.PaymentCardID != nil {
		createStrings = append(createStrings, `"PaymentCardID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PaymentCardID)
	}
	if req.PaymentCardExpMonth != nil {
		createStrings = append(createStrings, `"PaymentCardExpMonth"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PaymentCardExpMonth)
	}
	if req.PaymentCardExpYear != nil {
		createStrings = append(createStrings, `"PaymentCardExpYear"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.PaymentCardExpYear)
	}
	if req.AuthNo != nil {
		createStrings = append(createStrings, `"AuthNo"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.AuthNo)
	}
	if req.UserKey != nil {
		createStrings = append(createStrings, `"UserKey"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, *req.UserKey)
	}

	if len(args) != 0 {
		createStrings = append(createStrings, "created_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())

		createStrings = append(createStrings, "updated_at")
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, time.Now())
	}

	concatedCreateString := strings.Join(createStrings, ",")
	concatedNumString := strings.Join(numString, ",")

	return concatedCreateString, concatedNumString, args
}
