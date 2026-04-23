package api

import (
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type OrdersAPI struct {
	repo           repo.OrdersRepository
	profileService profiles.ProfileService
	priorityClient *priority.Client
}

func NewOrdersAPI(db repo.OrdersRepository) *OrdersAPI {
	return &OrdersAPI{
		repo:           db,
		profileService: profiles.NewProfileServiceAPI(keycloak.NewClient()),
		priorityClient: priority.NewClient(),
	}
}

func (o *OrdersAPI) SetProfileService(ps profiles.ProfileService) {
	o.profileService = ps
}

func (o *OrdersAPI) SetPriorityClient(c *priority.Client) {
	o.priorityClient = c
}
