package common

import "github.com/kelseyhightower/envconfig"

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

	PelecardUser              string `envconfig:"PELECARD_USER"`
	PelecardPassword          string `envconfig:"PELECARD_PASSWORD"`
	PelecardNewTerminalNumber string `envconfig:"PELECARD_NEW_TERMINAL_NUMBER"`
	PelecardOldTerminalNumber string `envconfig:"PELECARD_OLD_TERMINAL_NUMBER"`

	KeycloakServerUrl    string `envconfig:"KEYCLOAK_SERVER_URL"`
	KeycloakRealm        string `envconfig:"KEYCLOAK_REALM"`
	KeycloakClientID     string `envconfig:"KEYCLOAK_CLIENT_ID"`
	KeycloakClientSecret string `envconfig:"KEYCLOAK_CLIENT_SECRET"`

	ProfileServiceUrl    string `envconfig:"PROFILE_SERVICE_URL"`
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
	envconfig.Process("LIST", Config)
}
