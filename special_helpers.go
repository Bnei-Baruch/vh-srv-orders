package main

import (
	"github.com/gin-gonic/gin"
)

func hardDeleteSpecialByEmail(c *gin.Context, email string) (error, int64) {
	hardDeleteSpecialRes, err := DB.Exec(c, "DELETE FROM specials WHERE email = $1", email)

	if err != nil {
		return err, 0
	}

	rowsAffected := hardDeleteSpecialRes.RowsAffected()

	return nil, rowsAffected
}

func getSpecialByEmail(ctx *gin.Context, email string) (Special, error) {
	var spe Special

	if err := DB.QueryRow(ctx,
		`SELECT 
		email,
		category,
		subcategory
	 	from specials where email = $1`, email).Scan(
		&spe.Email,
		&spe.Category,
		&spe.SubCategory,
	); err != nil {
		return spe, err
	}
	return spe, nil

}
