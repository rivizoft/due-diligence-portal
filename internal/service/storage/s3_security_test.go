package storage

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/witfoo/due-diligence-portal/internal/domain"
)

func TestS3DoesNotFollowRedirects(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodGet, http.MethodDelete} {
		for _, status := range []int{301, 302, 303, 307, 308} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				var requests atomic.Int32
				var received atomic.Int64
				var signed atomic.Bool
				sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requests.Add(1)
					signed.Store(r.Header.Get("Authorization") != "")
					data, _ := io.ReadAll(r.Body)
					received.Add(int64(len(data)))
					w.Header().Set("ETag", `"fixture"`)
					_, _ = w.Write([]byte("fixture"))
				}))
				defer sink.Close()
				origin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = io.Copy(io.Discard, r.Body)
					w.Header().Set("Location", sink.URL+"/unexpected-object")
					w.WriteHeader(status)
				}))
				defer origin.Close()
				client, err := newS3Client(&domain.StorageConfig{Region: "eu-central-1", Endpoint: origin.URL, PathStyle: true}, Credentials{AccessKeyID: "fixture-access", SecretAccessKey: "fixture-secret"})
				require.NoError(t, err)
				// Trust only the synthetic endpoint's certificate. Keep the production
				// client's redirect policy intact, including HTTPS-to-HTTP behavior.
				client.(*s3Client).client.Options().HTTPClient.(*http.Client).Transport = origin.Client().Transport
				ctx := context.Background()
				switch method {
				case http.MethodPut:
					err = client.Put(ctx, "private-bucket", "docs/fixture", []byte("confidential fixture"), "text/plain")
				case http.MethodGet:
					_, err = client.Get(ctx, "private-bucket", "docs/fixture", 100)
				case http.MethodDelete:
					err = client.Delete(ctx, "private-bucket", "docs/fixture")
				}
				require.Zero(t, requests.Load(), "redirect escaped HTTPS endpoint (signed=%t, uploaded bytes=%d)", signed.Load(), received.Load())
				require.Error(t, err)
			})
		}
	}
}
