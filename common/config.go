package common

import "github.com/kelseyhightower/envconfig"

type envConfig struct {
	Mode string `envconfig:"MODE"`
	Port string `envconfig:"PORT"`

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

	PelecardUser     string `envconfig:"PELECARD_USER"`
	PelecardPassword string `envconfig:"PELECARD_PASSWORD"`

	KeycloakServerUrl    string `envconfig:"KEYCLOAK_SERVER_URL"`
	KeycloakRealm        string `envconfig:"KEYCLOAK_REALM"`
	KeycloakClientID     string `envconfig:"KEYCLOAK_CLIENT_ID"`
	KeycloakClientSecret string `envconfig:"KEYCLOAK_CLIENT_SECRET"`

	ProfileServiceUrl    string `envconfig:"PROFILE_SERVICE_URL"`
	GoogleAppCredentials string `envconfig:"GOOGLE_APPLICATION_CREDENTIALS"`

	NatsUrl string `envconfig:"NATS_URL"`
}

var Config = new(envConfig)

func LoadConfig() {
	envconfig.Process("LIST", Config)
}
