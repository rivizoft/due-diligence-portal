package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"

	"github.com/witfoo/due-diligence-portal/internal/domain"
)

// Client describes the three object operations used by this application.
type Client interface {
	Put(context.Context, string, string, []byte, string) error
	Get(context.Context, string, string, int64) ([]byte, error)
	Delete(context.Context, string, string) error
}

// Factory constructs a client from explicit administrator-configured credentials.
type Factory func(*domain.StorageConfig, Credentials) (Client, error)
type s3Client struct{ client *s3.Client }

func newS3Client(config *domain.StorageConfig, creds Credentials) (Client, error) {
	cfg := aws.Config{Region: config.Region, Credentials: credentials.NewStaticCredentialsProvider(creds.AccessKeyID, creds.SecretAccessKey, ""), RetryMaxAttempts: 2, HTTPClient: &http.Client{Timeout: 60 * time.Second}}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = config.PathStyle
		if config.Endpoint != "" {
			o.BaseEndpoint = aws.String(config.Endpoint)
		}
	})
	return &s3Client{client: client}, nil
}
func (c *s3Client) Put(ctx context.Context, bucket, key string, data []byte, mime string) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), Body: bytes.NewReader(data), ContentLength: aws.Int64(int64(len(data))), ContentType: aws.String(mime)})
	return err
}
func (c *s3Client) Get(ctx context.Context, bucket, key string, maxBytes int64) ([]byte, error) {
	if maxBytes < 0 || maxBytes > 2*1024*1024*1024 {
		return nil, ErrContent
	}
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NotFound") {
			return nil, ErrContent
		}
		return nil, err
	}
	defer func() { _ = out.Body.Close() }()
	if out.ContentLength != nil && *out.ContentLength > maxBytes {
		return nil, ErrContent
	}
	data, err := io.ReadAll(io.LimitReader(out.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, ErrContent
	}
	return data, nil
}
func (c *s3Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NotFound") {
			return nil
		}
	}
	return err
}
