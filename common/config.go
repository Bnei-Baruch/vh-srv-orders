package common

import (
	"log/slog"
	"os"

	"github.com/kelseyhightower/envconfig"
)

type envConfig struct {
	Mode string `envconfig:"APP_MODE"`
	Port string `envconfig:"APP_PORT"`
	Env  string `envconfig:"APP_ENV"`

	PgHost   string `envconfig:"PGHOST"`
	PgPort   string `envconfig:"PGPORT"`
	PgUser   string `envconfig:"PGUSER"`
	PgPass   string `envconfig:"PGPASSWORD"`
	PgDbName string `envconfig:"PGDATABASE"`

	S3SecretKey  string `envconfig:"S3_SECRET_KEY"`
	S3AccesstKey string `envconfig:"S3_ACCESS_KEY"`
	S3Region     string `envconfig:"S3_REGION"`
	S3BucketName string `envconfig:"S3_BUCKET_NAME"`
	S3Endpoint   string `envconfig:"S3_ENDPOINT"`

	// The Keycloak client also identifies this service to external_payments,
	// which resolves it to a registered caller carrying an organization.
	//
	// That does not replace the organization in the request body: it is required
	// there and is sent on every charge (Organization: "ben2",
	// domain/billing/renewal.go). Where both arrive the key is what the other
	// side trusts, and a disagreement is logged there as ORG MISMATCH — so the
	// credential decides which merchant account is charged, and the body field
	// has to agree with it rather than being the thing that chooses.
	KeycloakServerUrl    string `envconfig:"KEYCLOAK_SERVER_URL"`
	KeycloakRealm        string `envconfig:"KEYCLOAK_REALM"`
	KeycloakClientID     string `envconfig:"KEYCLOAK_CLIENT_ID"`
	KeycloakClientSecret string `envconfig:"KEYCLOAK_CLIENT_SECRET"`

	ProfileServiceUrl    string `envconfig:"PROFILE_SERVICE_URL"`
	AccountingServiceUrl string `envconfig:"ACCOUNTING_SERVICE_URL"`
	QuickbooksCompanyID  string `envconfig:"QUICKBOOKS_COMPANY_ID"`
	GoogleAppCredentials string `envconfig:"GOOGLE_APPLICATION_CREDENTIALS"`

	NatsUrl                     string `envconfig:"NATS_URL"`
	ImportSpecialsSpreadsheetId string `envconfig:"IMPORT_SPECIALS_SPREADSHEET_ID"`

	// Debug flag: Ignore admin roles in permission checks (for testing security)
	DebugIgnoreAdminRoles bool `envconfig:"DEBUG_IGNORE_ADMIN_ROLES" default:"false"`

	RenewalMaxWorkers int `envconfig:"RENEWAL_MAX_WORKERS" default:"5"`

	PriorityBaseURL  string `envconfig:"PRIORITY_BASE_URL"`
	PriorityUsername string `envconfig:"PRIORITY_USERNAME"`
	PriorityPassword string `envconfig:"PRIORITY_PASSWORD"`
}

var Config = new(envConfig)

func LoadConfig() {
	if err := envconfig.Process("LIST", Config); err != nil {
		slog.Error("envconfig.Process", slog.Any("err", err))
		os.Exit(1)
	}
}
