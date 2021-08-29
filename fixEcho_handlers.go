package main

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

func handleFixEcho(c *gin.Context) {
	type req struct {
		Record []string `json:"records"`
	}
	var body req

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"Error": err.Error()})
	}

	// loop over the records
	// for each record
	//   - cancel the payment where paramx eq record item
	//   - get orderID of given record, mark record torenew
	//   - get accoundID of given order, and send email
	//
	var total int
	for i := 0; i < len(body.Record); i++ {

		fmt.Println(body.Record[i])

		total = i

	}

	c.JSON(http.StatusOK, gin.H{"Success": total})

}
