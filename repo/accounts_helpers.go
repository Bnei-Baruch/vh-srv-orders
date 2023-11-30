package repo

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v4"
)

// CreateOrUpdateAccount account
func (o *OrdersDB) CreateOrUpdateAccount(ctx context.Context, a Account) int64 {
	var b Account
	reqAccountExist := `
		select id from accounts where "UserKey" = $1 ORDER BY id DESC LIMIT 1
	`
	fmt.Println("--account-struct--", a)
	if err := o.QueryRow(ctx, reqAccountExist, a.UserKey.String).Scan(
		&b.ID,
	); err != nil {
		if err == pgx.ErrNoRows {

			createString, numString, createQueryArgs := prepareAccountCreateQuery(a)

			var ID int64
			// Add new account if not exist
			if len(createQueryArgs) != 0 {
				if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO accounts (%s) VALUES (%s) RETURNING id`, createString, numString),
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

func (o *OrdersDB) CreateAccount(ctx context.Context, a Account) (int, error) {

	createString, numString, createQueryArgs := prepareAccountCreateQuery(a)

	var ID int

	if len(createQueryArgs) != 0 {
		if err := o.QueryRow(ctx, fmt.Sprintf(`INSERT INTO accounts (%s) VALUES (%s) RETURNING id`, createString, numString),
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

func (o *OrdersDB) GetAllAccounts(ctx context.Context, skip int, limit int, email string) (*[]Account, error) {

	accounts := []Account{}

	limitOffsetString := fmt.Sprintf(" LIMIT %d OFFSET %d", limit, skip)

	whereQuery, orderByQuery, queryBuildErr := buildAndGetAccountsWhereQuery(email)

	if queryBuildErr != nil {
		return &accounts, queryBuildErr
	}

	rows, dbErr := o.Query(ctx, `
		SELECT
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
		deleted_at
			FROM accounts`+whereQuery+orderByQuery+limitOffsetString)

	if dbErr != nil {
		fmt.Println("error in get all accounts", dbErr)
		return &accounts, dbErr
	}

	defer rows.Close()
	for rows.Next() {
		var a Account
		if err := rows.Scan(
			&a.ID,
			&a.FirstName,
			&a.LastName,
			&a.Email,
			&a.Phone,
			&a.Street,
			&a.City,
			&a.State,
			&a.Postcode,
			&a.Country,
			&a.AccountType,
			&a.PaymentToken,
			&a.PaymentCardID,
			&a.PaymentCardExpMonth,
			&a.PaymentCardExpYear,
			&a.UserKey,
			&a.AuthNo,
			&a.CreatedAt,
			&a.UpdatedAt,
			&a.DeletedAt,
		); err != nil {
			fmt.Println("error in get all accounts", err)
			return &accounts, err
		}
		accounts = append(accounts, a)
	}

	return &accounts, nil
}

func (o *OrdersDB) PatchAccount(ctx context.Context, req Account, accountID int) error {

	toUpdate, toUpdateArgs := prepareAccountUpdateQuery(req)

	if len(toUpdateArgs) != 0 {
		updateRes, err := o.Exec(ctx, fmt.Sprintf(`UPDATE accounts SET %s WHERE id=%d`, toUpdate, accountID),
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

func (o *OrdersDB) SoftDeleteAccount(ctx context.Context, accountID int) error {
	_, err := o.Exec(ctx, "UPDATE accounts SET deleted_at = $1 WHERE id = $2", time.Now(), accountID)
	return err
}

func (o *OrdersDB) HardDeleteAllUserDataByAccountID(ctx context.Context, accountID int, kc_id string) error {

	// start transaction
	tx, err := o.Begin(ctx)
	if err != nil {
		return err
	}

	defer tx.Rollback(ctx)

	if accountID != 0 {
		// delete all user data
		_, err = tx.Exec(ctx, `DELETE FROM transaction WHERE account_id = $1`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM card_details where account_id = $1`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments_helphaver where payment_id in (SELECT id FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" = $1))`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments_offline where payment_id in (SELECT id FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" = $1))`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments_pelecard where payment_id in (SELECT id FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" = $1))`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM specials where email = (SELECT "Email" FROM accounts WHERE id = $1)`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" = $1)`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM orders where "AccountID" = $1`, accountID)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, "DELETE FROM accounts WHERE id = $1", accountID)
		if err != nil {
			return err
		}

	} else if kc_id != "" {
		// delete all user data
		_, err = tx.Exec(ctx, `DELETE FROM transaction WHERE account_id in (SELECT id FROM accounts WHERE "UserKey" = $1)`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM card_details where account_id in (SELECT id FROM accounts WHERE "UserKey" = $1)`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments_helphaver where payment_id in (SELECT id FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" in (SELECT id FROM accounts WHERE "UserKey" = $1)))`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments_offline where payment_id in (SELECT id FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" in (SELECT id FROM accounts WHERE "UserKey" = $1)))`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments_pelecard where payment_id in (SELECT id FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" in (SELECT id FROM accounts WHERE "UserKey" = $1)))`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM specials where email in (SELECT "Email" FROM accounts WHERE "UserKey" = $1)`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM payments where "OrderID" in (SELECT id FROM orders where "AccountID" in (SELECT id FROM accounts WHERE "UserKey" = $1))`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM orders where "AccountID" in (SELECT id FROM accounts WHERE "UserKey" = $1)`, kc_id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `DELETE FROM accounts WHERE "UserKey" = $1`, kc_id)
		if err != nil {
			return err
		}

	} else {
		return errors.New("accountID and kc_id are both empty")
	}

	return tx.Commit(ctx)

}

func (o *OrdersDB) GetAccount(ctx context.Context, id int, email string) (Account, error) {
	var (
		acc        Account
		whereQuery string
	)

	if id != 0 {
		whereQuery = fmt.Sprintf("where id = %d", id)
	} else {
		whereQuery = fmt.Sprintf("where \"Email\" = '%s'", email)
	}

	if err := o.QueryRow(ctx, `SELECT 
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

func buildAndGetAccountsWhereQuery(email string) (string, string, error) {

	var whereString strings.Builder
	var orderBy strings.Builder
	var whereCondition strings.Builder
	whereString.WriteString(" WHERE")
	whereCondition.WriteString("")

	if email != "" {
		if whereCondition.String() != "" {
			whereCondition.WriteString(" AND")
		}
		whereCondition.WriteString(fmt.Sprintf(` "Email" = '%s'`, email))
	}

	orderBy.WriteString(fmt.Sprintf(" ORDER BY updated_at %s", "desc"))

	if whereCondition.String() != "" {
		whereString.WriteString(whereCondition.String())
	} else {
		whereString.Reset()
	}
	return whereString.String(), orderBy.String(), nil
}
