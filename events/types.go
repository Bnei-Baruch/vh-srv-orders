package events

import (
	"io"
	"math/rand"
	"time"

	"github.com/oklog/ulid/v2"
	"gitlab.bbdev.team/vh/pay/orders/common"
)

const (
	ActorSystem = common.ServiceName

	ComponentAPI                     = "api"
	ComponentOfflinePaymentsImporter = "offline_payments_importer"
	ComponentRobokasaImporter        = "robokasa_importer"
	ComponentSpecialImporter         = "special_importer"
	ComponentSpecialActivator        = "special_activator"
	ComponentProfileEventHandler     = "profile_event_handler"

	TypeCreateAccount     = "create_account"
	TypeUpdateAccount     = "update_account"
	TypeMergeAccount      = "merge_account"
	TypeDeleteAccount     = "delete_account"
	TypeHardDeleteAccount = "hard_delete_account"
	TypeCreateOrder       = "create_order"
	TypeUpdateOrder       = "update_order"
	TypeDeleteOrder       = "delete_order"
	TypeCreatePayment     = "create_payment"
	TypeUpdatePayment     = "update_payment"
	TypeDeletePayment     = "delete_payment"
	TypeDeleteSpecial     = "delete_special"
	TypeCreateSpecial     = "create_special"
)

var entropy io.Reader

func init() {
	entropy = rand.New(rand.NewSource(time.Now().UTC().UnixNano()))
}

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Component string                 `json:"component"`
	Actor     string                 `json:"actor"`
	RequestID string                 `json:"request_id,omitempty"`
	Payload   map[string]interface{} `json:"payload"`
}

type EventBuilder interface {
	BuildEvent(eventType string, payload map[string]interface{}) Event
}

func MakeEvent(eventType string, payload map[string]interface{}) Event {
	return Event{
		ID:        ulid.MustNew(ulid.Now(), entropy).String(),
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now().UTC(),
	}
}
