package profilestest

import (
	"time"

	uuid "github.com/satori/go.uuid"

	"gitlab.bbdev.team/vh/pay/orders/pkg/profiles"
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
		KeycloakID:          new(uuid.NewV4()),
		UserID:              new(uuid.NewV4()),
		PrimaryEmail:        new("user@example.com"),
		UpdatedAt:           time.Now(),
		CreatedAt:           time.Now().AddDate(-1, 0, 0),
		Deleted:             false,
		Status:              profiles.Status{},
		FirstNameLatin:      new("FirstNameLatin"),
		FirstNameVernacular: new("FirstNameVernacular"),
		LastNameLatin:       new("LastNameLatin"),
		LastNameVernacular:  new("LastNameVernacular"),
		StreetAddress:       new("StreetAddress"),
		Country:             new("Country"),
		StateOrRegion:       new("StateOrRegion"),
		PostalCode:          new("PostalCode"),
		City:                new("City"),
		Gender:              new("Gender"),
		MaritalStatus:       new("MaritalStatus"),
		DateOfBirth:         new("DateOfBirth"),
		AlternateEmail1:     new("AlternateEmail1"),
		AlternateEmail2:     new("AlternateEmail2"),
		MobileNumber:        new("MobileNumber"),
		WhatsAppNumber:      new("WhatsAppNumber"),
		TelegramNumber:      new("TelegramNumber"),
		FirstLanguage:       new("FirstLanguage"),
		OtherLanguage1:      new("OtherLanguage1"),
		OtherLanguage2:      new("OtherLanguage2"),
		OtherLanguage3:      new("OtherLanguage3"),
		OtherLanguage4:      new("OtherLanguage4"),
		ListeningLanguage:   new("ListeningLanguage"),
		ReadingLanguage:     new("ReadingLanguage"),
		EmailLanguage:       new("EmailLanguage"),
		StudyStartYear:      new(2020),
		StudyFramework:      new("StudyFramework"),
		HasGroup:            new(true),
		WantsGroup:          new(false),
		NameOfGroup:         new("NameOfGroup"),
	}
}
