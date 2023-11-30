package common

import "github.com/kelseyhightower/envconfig"

type envConfig struct {
	Mode             string `envconfig:"MODE"`
	Port             string `envconfig:"PORT"`
	PgHost           string `envconfig:"PGHOST"`
	PgPort           string `envconfig:"PGPORT"`
	PgUser           string `envconfig:"PGUSER"`
	PgPass           string `envconfig:"PGPASSWORD"`
	PgDbName         string `envconfig:"PGDATABASE"`
	S3SecretKey      string `envconfig:"S3_SECRET_KEY"`
	S3AccesstKey     string `envconfig:"S3_ACCESS_KEY"`
	S3Region         string `envconfig:"S3_REGION"`
	S3BucketName     string `envconfig:"S3_BUCKET_NAME"`
	S3Endpoint       string `envconfig:"S3_ENDPOINT"`
	PelecardUser     string `envconfig:"PELECARD_USER"`
	PelecardPassword string `envconfig:"PELECARD_PASSWORD"`
}

var Config = new(envConfig)

func LoadConfig() {
	envconfig.Process("LIST", Config)
}
