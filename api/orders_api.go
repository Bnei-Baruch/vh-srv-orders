package api

import (
	"gitlab.bbdev.team/vh/pay/orders/common"
	"gitlab.bbdev.team/vh/pay/orders/pkg/accounting"
	"gitlab.bbdev.team/vh/pay/orders/pkg/keycloak"
	"gitlab.bbdev.team/vh/pay/orders/pkg/priority"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/repo"
)

type OrdersAPI struct {
	repo                repo.OrdersRepository
	profileService      profiles.ProfileService
	priorityClient      *priority.Client
	accountingService   accounting.AccountingService
	quickbooksCompanyID string
}

func NewOrdersAPI(db repo.OrdersRepository) *OrdersAPI {
	return &OrdersAPI{
		repo:                db,
		profileService:      profiles.NewProfileServiceAPI(keycloak.NewClient()),
		priorityClient:      priority.NewClient(),
		accountingService:   accounting.NewAccountingServiceAPI(keycloak.NewClient()),
		quickbooksCompanyID: common.Config.QuickbooksCompanyID,
	}
}

func (o *OrdersAPI) SetProfileService(ps profiles.ProfileService) {
	o.profileService = ps
}

func (o *OrdersAPI) SetPriorityClient(c *priority.Client) {
	o.priorityClient = c
}

func (o *OrdersAPI) SetAccountingService(s accounting.AccountingService) {
	o.accountingService = s
}

func (o *OrdersAPI) SetQuickbooksCompanyID(id string) {
	o.quickbooksCompanyID = id
}
