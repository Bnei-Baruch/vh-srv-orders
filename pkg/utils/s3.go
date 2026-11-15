package utils

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

// This type and var allows mocking this function for tests where we don't
// want to uploading anything anywhere.
type UploadFileToS3Func func(buffer []byte, fileName string) (string, error)

var UploadFileToS3 = uploadFileToS3

// uploadFileToS3 puts buffer at fileName in the configured bucket, publicly
// readable, and returns its URL.
func uploadFileToS3(buffer []byte, fileName string) (string, error) {
	client := s3.New(s3.Options{
		Credentials:  credentials.NewStaticCredentialsProvider(common.Config.S3AccesstKey, common.Config.S3SecretKey, ""),
		BaseEndpoint: aws.String(s3BaseEndpoint(common.Config.S3Endpoint)),
		Region:       common.Config.S3Region,
		UsePathStyle: true,
		// The endpoint is an S3-compatible store, not AWS: send checksums only
		// where the API requires them, as v1 did, since not every store
		// accepts the CRC32 headers v2 adds by default.
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})

	_, err := client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket:             aws.String(common.Config.S3BucketName),
		Key:                aws.String(fileName),
		ACL:                types.ObjectCannedACLPublicRead,
		Body:               bytes.NewReader(buffer),
		ContentLength:      aws.Int64(int64(len(buffer))),
		ContentType:        aws.String(http.DetectContentType(buffer)),
		ContentDisposition: aws.String("attachment"),
	})
	if err != nil {
		return "", fmt.Errorf("client.PutObject: %w", err)
	}

	return common.Config.S3Endpoint + "/" + common.Config.S3BucketName + "/" + fileName, nil
}

// s3BaseEndpoint gives a scheme-less S3_ENDPOINT the http:// that v1's
// DisableSSL used to add; v2 requires a full URL.
func s3BaseEndpoint(endpoint string) string {
	if strings.Contains(endpoint, "://") {
		return endpoint
	}
	return "http://" + endpoint
}
