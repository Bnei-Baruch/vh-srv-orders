package profilestest

import (
	"time"

	uuid "github.com/satori/go.uuid"
	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
	"gitlab.bbdev.team/vh/pay/orders/pkg/utils"
)

func EventFixture(eventType string, payload map[string]interface{}) profiles.Event {
	return profiles.Event{
		Type:      eventType,
		Payload:   payload,
		ID:        "test_id",
		Timestamp: time.Now(),
		Component: "tests_component",
		Actor:     "tests_actor",
	}
}

func ProfileFixture() *profiles.Profile {
	return &profiles.Profile{
		KeycloakID:          utils.PointerUUID(uuid.NewV4()),
		UserID:              utils.PointerUUID(uuid.NewV4()),
		PrimaryEmail:        utils.PointerString("user@example.com"),
		UpdatedAt:           time.Now(),
		CreatedAt:           time.Now().AddDate(-1, 0, 0),
		Deleted:             false,
		Status:              profiles.Status{},
		FirstNameLatin:      utils.PointerString("FirstNameLatin"),
		FirstNameVernacular: utils.PointerString("FirstNameVernacular"),
		LastNameLatin:       utils.PointerString("LastNameLatin"),
		LastNameVernacular:  utils.PointerString("LastNameVernacular"),
		StreetAddress:       utils.PointerString("StreetAddress"),
		Country:             utils.PointerString("Country"),
		StateOrRegion:       utils.PointerString("StateOrRegion"),
		PostalCode:          utils.PointerString("PostalCode"),
		City:                utils.PointerString("City"),
		Gender:              utils.PointerString("Gender"),
		MaritalStatus:       utils.PointerString("MaritalStatus"),
		DateOfBirth:         utils.PointerString("DateOfBirth"),
		AlternateEmail1:     utils.PointerString("AlternateEmail1"),
		AlternateEmail2:     utils.PointerString("AlternateEmail2"),
		MobileNumber:        utils.PointerString("MobileNumber"),
		WhatsAppNumber:      utils.PointerString("WhatsAppNumber"),
		TelegramNumber:      utils.PointerString("TelegramNumber"),
		FirstLanguage:       utils.PointerString("FirstLanguage"),
		OtherLanguage1:      utils.PointerString("OtherLanguage1"),
		OtherLanguage2:      utils.PointerString("OtherLanguage2"),
		OtherLanguage3:      utils.PointerString("OtherLanguage3"),
		OtherLanguage4:      utils.PointerString("OtherLanguage4"),
		ListeningLanguage:   utils.PointerString("ListeningLanguage"),
		ReadingLanguage:     utils.PointerString("ReadingLanguage"),
		EmailLanguage:       utils.PointerString("EmailLanguage"),
		StudyStartYear:      utils.PointerInt(2020),
		StudyFramework:      utils.PointerString("StudyFramework"),
		HasGroup:            utils.PointerBool(true),
		WantsGroup:          utils.PointerBool(false),
		NameOfGroup:         utils.PointerString("NameOfGroup"),
	}
}
