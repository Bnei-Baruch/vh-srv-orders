package utils

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/kelseyhightower/envconfig"
)

type envConfig struct {
	PgHost        string `envconfig:"PGHOST" default:"localhost"`
	PgPort        string `envconfig:"PGPORT" default:"5432"`
	PgUser        string `envconfig:"PGUSER" default:"postgres"`
	PgPass        string `envconfig:"PGPASSWORD" default:"pass"`
	PgDbName      string `envconfig:"PGDATABASE" default:"gorm"`
	AWSSecretKey  string `envconfig:"AWS_SECRET_KEY"`
	AWSAccesstKey string `envconfig:"AWS_ACCESS_KEY"`
	AWSRegion     string `envconfig:"AWS_REGION"`
	AWSBucketName string `envconfig:"AWS_BUCKET_NAME"`
}

// UploadFileToS3 will upload a single file to S3, it will require a file buffer and filename
// It'll will set file info like content type and encryption on the uploaded file.
func UploadFileToS3(buffer []byte, fileName string) (string, error) {

	envCgf, envCfgErr := getEnvVariables()

	if envCfgErr != nil {
		fmt.Println("Error while fetching env file")
		return "", envCfgErr
	}

	appCreds := aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(envCgf.AWSAccesstKey, envCgf.AWSSecretKey, ""))
	_, retErr := appCreds.Retrieve(context.TODO())
	if retErr != nil {
		fmt.Println("Error while fetching credentials")
		return "", retErr
	}

	cfg, cfgErr := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(envCgf.AWSRegion),
	)
	if cfgErr != nil {
		fmt.Println("Error while fetching config")
		return "", cfgErr
	}

	client := s3.NewFromConfig(cfg)

	// Uploading the file to S3.
	_, uploadErr := client.PutObject(context.TODO(), &s3.PutObjectInput{
		Bucket:               aws.String(envCgf.AWSBucketName),
		Key:                  aws.String(fileName),
		ACL:                  "public-read",
		Body:                 bytes.NewReader(buffer),
		ContentLength:        int64(len(buffer)),
		ContentType:          aws.String(http.DetectContentType(buffer)),
		ContentDisposition:   aws.String("attachment"),
		ServerSideEncryption: "AES256",
	})

	if uploadErr != nil {
		fmt.Println("Error uploading to S3", uploadErr)
		return "", uploadErr
	}

	uploadedFileUrl := "https://" + envCgf.AWSBucketName + ".s3.amazonaws.com/" + fileName

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
