package utils

import (
	"bytes"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"

	"gitlab.bbdev.team/vh/pay/orders/common"
)

// UploadFileToS3 will upload a single file to S3, it will require a file buffer and filename
// It'll will set file info like content type and encryption on the uploaded file.
// TODO (edo): use io.Reader instead of []byte
func UploadFileToS3(buffer []byte, fileName string) (string, error) {
	// Creating a new session with the given configuration.
	s3Config := &aws.Config{
		Credentials:      credentials.NewStaticCredentials(common.Config.S3AccesstKey, common.Config.S3SecretKey, ""),
		Endpoint:         aws.String(common.Config.S3Endpoint),
		Region:           aws.String(common.Config.S3Region),
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
		Bucket:             aws.String(common.Config.S3BucketName),
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
	uploadedFileUrl := common.Config.S3Endpoint + "/" + common.Config.S3BucketName + "/" + fileName

	return uploadedFileUrl, nil
}
