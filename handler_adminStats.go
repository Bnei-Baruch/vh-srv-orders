package main

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

//RequestStats is query for stats
type RequestStats struct {
	Product string `json:"product" binding:"required"`
	Year    string `json:"year" binding:"required"`
}

//ResponseStats is reponse from stats
type ResponseStats struct {
	Data []StatsMonth `json:"data"`
}

//StatsMonth are stats for a month
type StatsMonth struct {
	Month    string              `json:"month"`
	Year     string              `json:"year"`
	Products BreakdownByCurrency `json:"products"`
	Orders   BreakdownByCurrency `json:"orders"`
}

//BreakdownByCurrency is breakdown by currency
type BreakdownByCurrency struct {
	USD   string `json:"USD"`
	NIS   string `json:"NIS"`
	EUR   string `json:"EUR"`
	Total string `json:"total"`
}

func handleAdminStats(c *gin.Context) {
	var req RequestStats

	err := c.BindJSON(&req)

	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var resp ResponseStats
	data := []StatsMonth{GetStatsForMonth(1, "2021")}

	resp.Data = data

	if err != nil {
		log.Println(err.Error())
		c.JSON(http.StatusInternalServerError, gin.H{"Error": err.Error()})
	}

	c.JSON(http.StatusOK, resp)
}

//GetStatsForMonth provide stats for months
func GetStatsForMonth(month int, year string) StatsMonth {
	stats := StatsMonth{
		Month:    time.Month(month).String(),
		Year:     year,
		Products: GetProductBreakdownByCurrency(month, year),
		Orders:   GetOrderBreakdownByCurrency(month, year),
	}

	return stats
}

//GetProductBreakdownByCurrency provide produtcs by currency
func GetProductBreakdownByCurrency(month int, year string) BreakdownByCurrency {
	// replace with query
	pbc := BreakdownByCurrency{
		USD:   "2121",
		NIS:   "23232",
		EUR:   "3232",
		Total: "323232",
	}
	return pbc
}

//GetOrderBreakdownByCurrency provide orders by currency
func GetOrderBreakdownByCurrency(month int, year string) BreakdownByCurrency {
	obc := BreakdownByCurrency{
		USD:   "2121",
		NIS:   "23232",
		EUR:   "3232",
		Total: "323232",
	}
	return obc
}
