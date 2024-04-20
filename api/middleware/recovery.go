package middleware

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, err any) {
		switch v := err.(type) {
		case string:
			c.Error(errors.New(v))
		case error:
			c.Error(v)
		default:
		}

		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
