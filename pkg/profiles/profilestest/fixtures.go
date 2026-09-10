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
		KeycloakID:          utils.Ptr(uuid.NewV4()),
		UserID:              utils.Ptr(uuid.NewV4()),
		PrimaryEmail:        utils.Ptr("user@example.com"),
		UpdatedAt:           time.Now(),
		CreatedAt:           time.Now().AddDate(-1, 0, 0),
		Deleted:             false,
		Status:              profiles.Status{},
		FirstNameLatin:      utils.Ptr("FirstNameLatin"),
		FirstNameVernacular: utils.Ptr("FirstNameVernacular"),
		LastNameLatin:       utils.Ptr("LastNameLatin"),
		LastNameVernacular:  utils.Ptr("LastNameVernacular"),
		StreetAddress:       utils.Ptr("StreetAddress"),
		Country:             utils.Ptr("Country"),
		StateOrRegion:       utils.Ptr("StateOrRegion"),
		PostalCode:          utils.Ptr("PostalCode"),
		City:                utils.Ptr("City"),
		Gender:              utils.Ptr("Gender"),
		MaritalStatus:       utils.Ptr("MaritalStatus"),
		DateOfBirth:         utils.Ptr("DateOfBirth"),
		AlternateEmail1:     utils.Ptr("AlternateEmail1"),
		AlternateEmail2:     utils.Ptr("AlternateEmail2"),
		MobileNumber:        utils.Ptr("MobileNumber"),
		WhatsAppNumber:      utils.Ptr("WhatsAppNumber"),
		TelegramNumber:      utils.Ptr("TelegramNumber"),
		FirstLanguage:       utils.Ptr("FirstLanguage"),
		OtherLanguage1:      utils.Ptr("OtherLanguage1"),
		OtherLanguage2:      utils.Ptr("OtherLanguage2"),
		OtherLanguage3:      utils.Ptr("OtherLanguage3"),
		OtherLanguage4:      utils.Ptr("OtherLanguage4"),
		ListeningLanguage:   utils.Ptr("ListeningLanguage"),
		ReadingLanguage:     utils.Ptr("ReadingLanguage"),
		EmailLanguage:       utils.Ptr("EmailLanguage"),
		StudyStartYear:      utils.Ptr(2020),
		StudyFramework:      utils.Ptr("StudyFramework"),
		HasGroup:            utils.Ptr(true),
		WantsGroup:          utils.Ptr(false),
		NameOfGroup:         utils.Ptr("NameOfGroup"),
	}
}
