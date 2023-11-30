package api

import "gitlab.bbdev.team/vh/pay/orders/repo"

type OrdersAPI struct {
	repo repo.OrdersRepository
}

func NewOrdersAPI(db repo.OrdersRepository) *OrdersAPI {
	return &OrdersAPI{
		repo: db,
	}
}
