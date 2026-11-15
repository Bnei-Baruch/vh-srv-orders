package utils

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Bnei-Baruch/vh-srv-orders/common"
)

type capturedPut struct {
	method, path, acl, contentType, disposition, body string
}

func fakeS3(t *testing.T, status int) (*httptest.Server, *capturedPut) {
	t.Helper()
	got := &capturedPut{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		*got = capturedPut{r.Method, r.URL.Path, r.Header.Get("X-Amz-Acl"),
			r.Header.Get("Content-Type"), r.Header.Get("Content-Disposition"), string(raw)}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, got
}

func useS3Config(t *testing.T, endpoint string) {
	t.Helper()
	old := *common.Config
	t.Cleanup(func() { *common.Config = old })
	common.Config.S3Endpoint = endpoint
	common.Config.S3BucketName = "invoices"
	common.Config.S3Region = "us-east-1"
	common.Config.S3AccesstKey = "access"
	common.Config.S3SecretKey = "secret"
}

func TestUploadFileToS3_PutsAPublicAttachmentPathStyle(t *testing.T) {
	srv, got := fakeS3(t, http.StatusOK)
	useS3Config(t, srv.URL)

	url, err := uploadFileToS3([]byte("%PDF-1.4 receipt"), "r/1.pdf")

	require.NoError(t, err)
	assert.Equal(t, srv.URL+"/invoices/r/1.pdf", url)
	assert.Equal(t, http.MethodPut, got.method)
	assert.Equal(t, "/invoices/r/1.pdf", got.path)
	assert.Equal(t, "public-read", got.acl)
	assert.Equal(t, "application/pdf", got.contentType)
	assert.Equal(t, "attachment", got.disposition)
	assert.Equal(t, "%PDF-1.4 receipt", got.body)
}

// v1 with DisableSSL accepted an endpoint without a scheme; v2 does not.
func TestUploadFileToS3_AcceptsAnEndpointWithoutScheme(t *testing.T) {
	srv, got := fakeS3(t, http.StatusOK)
	useS3Config(t, strings.TrimPrefix(srv.URL, "http://"))

	_, err := uploadFileToS3([]byte("x"), "a.txt")

	require.NoError(t, err)
	assert.Equal(t, "/invoices/a.txt", got.path)
}

func TestUploadFileToS3_ReturnsTheStoreError(t *testing.T) {
	srv, _ := fakeS3(t, http.StatusForbidden)
	useS3Config(t, srv.URL)

	_, err := uploadFileToS3([]byte("x"), "a.txt")

	assert.Error(t, err)
}
