package storage

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/witfoo/due-diligence-portal/internal/domain"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestS3SDKSigningAndBoundedReads(t *testing.T) {
	var saved []byte
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Contains(t, r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=test-access/")
		require.Contains(t, r.Header.Get("Authorization"), "/eu-central-1/s3/aws4_request")
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.Method {
		case "PUT":
			saved, _ = io.ReadAll(r.Body)
			w.Header().Set("ETag", `"test"`)
		case "GET":
			_, _ = w.Write(saved)
		case "DELETE":
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	client, err := newS3Client(&domain.StorageConfig{Region: "eu-central-1", Endpoint: server.URL, PathStyle: true}, Credentials{AccessKeyID: "test-access", SecretAccessKey: "test-secret"})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, client.Put(ctx, "private-bucket", "docs/a b", []byte("payload"), "text/plain"))
	got, err := client.Get(ctx, "private-bucket", "docs/a b", 7)
	require.NoError(t, err)
	require.Equal(t, "payload", string(got))
	_, err = client.Get(ctx, "private-bucket", "docs/a b", 3)
	require.ErrorIs(t, err, ErrContent)
	require.NoError(t, client.Delete(ctx, "private-bucket", "docs/a b"))
	require.Equal(t, []string{"PUT /private-bucket/docs/a b", "GET /private-bucket/docs/a b", "GET /private-bucket/docs/a b", "DELETE /private-bucket/docs/a b"}, paths)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = client.Get(cancelled, "private-bucket", "key", 7)
	require.Error(t, err)
	require.False(t, strings.Contains(err.Error(), "test-secret"))
}
