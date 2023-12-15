package events

import "time"

const (
	ComponentAPI              = "api"
	ComponentRobokasaImporter = "robokasa_importer"

	TypeCreateAccount     = "create_account"
	TypeUpdateAccount     = "update_account"
	TypeDeleteAccount     = "delete_account"
	TypeHardDeleteAccount = "hard_delete_account"
	TypeCreateOrder       = "create_order"
	TypeUpdateOrder       = "update_order"
	TypeDeleteOrder       = "delete_order"
	TypeCreatePayment     = "create_payment"
	TypeUpdatePayment     = "update_payment"
	TypeDeletePayment     = "delete_payment"
	TypeDeleteSpecial     = "delete_special"
)

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	Component string                 `json:"component"`
	Actor     string                 `json:"actor"`
	Payload   map[string]interface{} `json:"payload"`
}

type EventBuilder interface {
	BuildEvent(eventType string, payload map[string]interface{}) Event
}

func MakeEvent(eventType string, payload map[string]interface{}) Event {
	return Event{Type: eventType, Payload: payload, Timestamp: time.Now().UTC()}
}
