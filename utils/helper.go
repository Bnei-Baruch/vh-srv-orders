package utils

import (
	"bytes"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/kelseyhightower/envconfig"
)

type envConfig struct {
	PgHost       string `envconfig:"PGHOST" default:"localhost"`
	PgPort       string `envconfig:"PGPORT" default:"5432"`
	PgUser       string `envconfig:"PGUSER" default:"postgres"`
	PgPass       string `envconfig:"PGPASSWORD" default:"pass"`
	PgDbName     string `envconfig:"PGDATABASE" default:"gorm"`
	S3SecretKey  string `envconfig:"S3_SECRET_KEY"`
	S3AccesstKey string `envconfig:"S3_ACCESS_KEY"`
	S3Region     string `envconfig:"S3_REGION"`
	S3BucketName string `envconfig:"S3_BUCKET_NAME"`
	S3Endpoint   string `envconfig:"S3_ENDPOINT"`
}

// UploadFileToS3 will upload a single file to S3, it will require a file buffer and filename
// It'll will set file info like content type and encryption on the uploaded file.
func UploadFileToS3(buffer []byte, fileName string) (string, error) {

	envCgf, envCfgErr := getEnvVariables()

	if envCfgErr != nil {
		fmt.Println("Error while fetching env file")
		return "", envCfgErr
	}

	// Creating a new session with the given configuration.
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(envCgf.S3AccesstKey, envCgf.S3SecretKey, ""),
		Endpoint:         aws.String(envCgf.S3Endpoint),
		Region:           aws.String(envCgf.S3Region),
		DisableSSL:       aws.Bool(true),
		S3ForcePathStyle: aws.Bool(true),
	}
	newSession, newSessErr := session.NewSession(s3Config)

	if newSessErr != nil {
		fmt.Println("Error while creating session")
		return "", newSessErr
	}

	s3Client := s3.New(newSession)

	// Uploading the file to S3.
	_, erre := s3Client.PutObject(&s3.PutObjectInput{
		Bucket:             aws.String(envCgf.S3BucketName),
		Key:                aws.String(fileName),
		ACL:                aws.String("public-read"),
		Body:               bytes.NewReader(buffer),
		ContentLength:      aws.Int64(int64(len(buffer))),
		ContentType:        aws.String(http.DetectContentType(buffer)),
		ContentDisposition: aws.String("attachment"),
	})

	if erre != nil {
		fmt.Printf("Failed to upload data to %s\n", erre.Error())
		return "", erre
	}

	// Creating a url for the uploaded file.
	uploadedFileUrl := envCgf.S3Endpoint + "/" + fileName

	return uploadedFileUrl, nil
}

func getEnvVariables() (envConfig, error) {

	var cfg envConfig

	if err := envconfig.Process("LIST", &cfg); err != nil {
		log.Fatalln("Error while fetching env file")
		return cfg, nil
	}

	return cfg, nil
}
