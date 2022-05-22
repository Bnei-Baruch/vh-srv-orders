package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v4"
)

// CreateOrUpdateAccount account
func CreateOrUpdateAccount(ctx *gin.Context, a Account) int64 {
	var b Account
	reqAccountExist := `
		select id from accounts where "UserKey" = $1 ORDER BY id DESC LIMIT 1
	`
	fmt.Println("--account-struct--", a)
	if err := DB.QueryRow(ctx, reqAccountExist, a.UserKey.String).Scan(
		&b.ID,
	); err != nil {
		if err == pgx.ErrNoRows {

			createString, numString, createQueryArgs := prepareAccountCreateQuery(a)

			var ID int64
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
	return b.ID
}

func createAccount(ctx *gin.Context, a Account) (int, error) {

	createString, numString, createQueryArgs := prepareAccountCreateQuery(a)

	var ID int

	if len(createQueryArgs) != 0 {
		if err := DB.QueryRow(ctx, fmt.Sprintf(`INSERT INTO accounts (%s) VALUES (%s) RETURNING id`, createString, numString),
			createQueryArgs...).Scan(
			&ID,
		); err != nil {
			return 0, err
		}
		return ID, nil
	} else {
		return 0, fmt.Errorf("invalid body")
	}

}

func patchAccount(c *gin.Context, req Account, accountID int) error {

	toUpdate, toUpdateArgs := prepareAccountUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := DB.Exec(c, fmt.Sprintf(`UPDATE accounts SET %s WHERE id=%d`, toUpdate, accountID),
			toUpdateArgs...)
		if err != nil {
			return fmt.Errorf("problem updating account: %w", err)
		}

		if updateRes.RowsAffected() == 0 {
			return fmt.Errorf("account not updated as no rows affected")
		}

	} else {
		fmt.Println("invalid values")
	}

	return nil
}

func getAccount(ctx *gin.Context, id int, email string) (Account, error) {
	var (
		acc        Account
		whereQuery string
	)

	if id != 0 {
		whereQuery = fmt.Sprintf("where id = %d", id)
	} else {
		whereQuery = fmt.Sprintf("where \"Email\" = '%s'", email)
	}

	if err := DB.QueryRow(ctx, `SELECT 
			id,
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
			"PaymentToken",
			"PaymentCardID",
			"PaymentCardExpMonth",
			"PaymentCardExpYear",
			"UserKey",
			"AuthNo",
			created_at,
			updated_at,
			deleted_at from accounts `+whereQuery).Scan(
		&acc.ID, &acc.FirstName, &acc.LastName, &acc.Email, &acc.Phone, &acc.Street,
		&acc.City, &acc.State, &acc.Postcode, &acc.Country, &acc.AccountType,
		&acc.PaymentToken, &acc.PaymentCardID, &acc.PaymentCardExpMonth, &acc.PaymentCardExpYear,
		&acc.UserKey, &acc.AuthNo, &acc.CreatedAt, &acc.UpdatedAt, &acc.DeletedAt,
	); err != nil {
		return acc, err
	}
	return acc, nil

}

func prepareAccountCreateQuery(req Account) (string, string, []interface{}) {
	var createStrings []string
	var numString []string
	var args []interface{}

	if req.FirstName.Valid {
		createStrings = append(createStrings, `"FirstName"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.FirstName.String)
	}
	if req.LastName.Valid {
		createStrings = append(createStrings, `"LastName"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.LastName.String)
	}
	if req.Email.Valid {
		createStrings = append(createStrings, `"Email"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Email.String)
	}
	if req.Phone.Valid {
		createStrings = append(createStrings, `"Phone"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Phone.String)
	}
	if req.Street.Valid {
		createStrings = append(createStrings, `"Street"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Street.String)
	}
	if req.City.Valid {
		createStrings = append(createStrings, `"City"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.City.String)
	}
	if req.State.Valid {
		createStrings = append(createStrings, `"State"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.State.String)
	}
	if req.Postcode.Valid {
		createStrings = append(createStrings, `"Postcode"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Postcode.String)
	}
	if req.Country.Valid {
		createStrings = append(createStrings, `"Country"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.Country.String)
	}
	if req.AccountType.Valid {
		createStrings = append(createStrings, `"AccountType"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AccountType.String)
	}
	if req.PaymentToken.Valid {
		createStrings = append(createStrings, `"PaymentToken"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentToken.String)
	}
	if req.PaymentCardID.Valid {
		createStrings = append(createStrings, `"PaymentCardID"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentCardID.String)
	}
	if req.PaymentCardExpMonth.Valid {
		createStrings = append(createStrings, `"PaymentCardExpMonth"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentCardExpMonth.Int64)
	}
	if req.PaymentCardExpYear.Valid {
		createStrings = append(createStrings, `"PaymentCardExpYear"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.PaymentCardExpYear.Int64)
	}
	if req.AuthNo.Valid {
		createStrings = append(createStrings, `"AuthNo"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.UserKey.Valid {
		createStrings = append(createStrings, `"UserKey"`)
		numString = append(numString, fmt.Sprintf("$%d", len(numString)+1))
		args = append(args, req.UserKey.String)
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

func prepareAccountUpdateQuery(req Account) (string, []interface{}) {
	var updateStrings []string
	var args []interface{}

	if req.FirstName.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"FirstName"=$%d`, len(updateStrings)+1))
		args = append(args, req.FirstName.String)
	}
	if req.LastName.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"LastName"=$%d`, len(updateStrings)+1))
		args = append(args, req.LastName.String)
	}
	if req.Email.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Email"=$%d`, len(updateStrings)+1))
		args = append(args, req.Email.String)
	}
	if req.Phone.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Phone"=$%d`, len(updateStrings)+1))
		args = append(args, req.Phone.String)
	}
	if req.Street.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Street"=$%d`, len(updateStrings)+1))
		args = append(args, req.Street.String)
	}
	if req.City.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"City"=$%d`, len(updateStrings)+1))
		args = append(args, req.City.String)
	}
	if req.State.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"State"=$%d`, len(updateStrings)+1))
		args = append(args, req.State.String)
	}
	if req.Postcode.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Postcode"=$%d`, len(updateStrings)+1))
		args = append(args, req.Postcode.String)
	}
	if req.Country.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`"Country"=$%d`, len(updateStrings)+1))
		args = append(args, req.Country.String)
	}
	if req.AccountType.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""AccountType""=$%d`, len(updateStrings)+1))
		args = append(args, req.AccountType.String)
	}
	if req.PaymentToken.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""PaymentToken""=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentToken.String)
	}
	if req.PaymentCardID.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""PaymentCardID""=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentCardID.String)
	}
	if req.PaymentCardExpMonth.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""PaymentCardExpMonth""=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentCardExpMonth.Int64)
	}
	if req.PaymentCardExpYear.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""PaymentCardExpYear""=$%d`, len(updateStrings)+1))
		args = append(args, req.PaymentCardExpYear.Int64)
	}
	if req.AuthNo.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""UserKey""=$%d`, len(updateStrings)+1))
		args = append(args, req.AuthNo.String)
	}
	if req.UserKey.Valid {
		updateStrings = append(updateStrings, fmt.Sprintf(`""AuthNo""=$%d`, len(updateStrings)+1))
		args = append(args, req.UserKey.String)
	}

	if len(args) != 0 {
		updateStrings = append(updateStrings, fmt.Sprintf("updated_at=$%d", len(updateStrings)+1))
		args = append(args, time.Now())
	}

	updateArgument := strings.Join(updateStrings, ",")

	return updateArgument, args
}
