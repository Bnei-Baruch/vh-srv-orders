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
