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
	// Concrete, because nothing substitutes it. *OrdersDB embeds *pgxpool.Pool,
	// so Exec/Query/Begin are promoted here too — writing SQL through them
	// skips OrdersDB.emitEvent. repo_surface_test.go guards that.
	repo                *repo.OrdersDB
	profileService      profiles.ProfileService
	priorityClient      *priority.Client
	accountingService   accounting.AccountingService
	quickbooksCompanyID string
}

func NewOrdersAPI(db *repo.OrdersDB) *OrdersAPI {
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
