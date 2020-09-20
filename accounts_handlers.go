package main

import (
	"fmt"      //
	"net/http" //

	"github.com/gin-gonic/gin"
)

func listAll(c *gin.Context) {
	var account []Account
	if err := DB.Find(&account).Error; err != nil {
		c.AbortWithStatus(404)
		fmt.Println(err)
	} else {
		c.JSON(http.StatusOK, account)
	}
}

func pingAccounts(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"ping": "pong"})
}

func echoAccounts(c *gin.Context) {
	var account Account
	c.BindJSON(&account)
	c.JSON(http.StatusOK, account)
}

func findByEmail(c *gin.Context) {
	email := c.Params.ByName("email")
	var account Account
	if err := DB.Where("email = ?", email).
		First(&account).Error; err != nil {
		c.AbortWithStatus(404)
		fmt.Println(err)
	} else {
		c.JSON(200, account)
	}
}

func new(c *gin.Context) {
	var account Account
	c.BindJSON(&account)
	DB.Create(&account)
	c.JSON(http.StatusOK, account)
}

func update(c *gin.Context) {
	var account Account
	id := c.Params.ByName("id")
	if err := DB.Where("id = ?", id).First(&account).Error; err != nil {
		c.AbortWithStatus(404)
		fmt.Println(err)
	}
	c.BindJSON(&account)
	DB.Save(&account)
	c.JSON(http.StatusOK, account)
}

func find(c *gin.Context) {
	id := c.Params.ByName("id")
	var account Account
	if err := DB.Where("id = ?", id).
		First(&account).Error; err != nil {
		c.AbortWithStatus(404)
		fmt.Println(err)
	} else {
		c.JSON(200, account)
	}
}

func delete(c *gin.Context) {
	id := c.Params.ByName("id")
	var account Account
	d := DB.Where("id = ?", id).Delete(&account)
	fmt.Println(d)
	c.JSON(200, gin.H{"id #" + id: "deleted"})
}

func handleCountAccounts(c *gin.Context) {
	total := countAccounts()
	c.JSON(200, gin.H{"total": total})
}

func handleAccountsTest(c *gin.Context) {
	//flagDuplicateOrders("notinuse")
	allaccounts, _ := GetAllAccountsWithDuplicates("plop")
	fmt.Println(len(allaccounts))
}
