// Package storage manages administrator-configured private object storage.
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/repository"
)

// Storage errors expose safe diagnostic messages without SDK request details.
var (
	ErrUnavailable    = errors.New("S3 is unavailable; check connection and object permissions")
	ErrLocationLocked = errors.New("bucket, endpoint, region and addressing cannot change while S3 versions or cleanup jobs exist")
	ErrContent        = errors.New("S3 original is missing or its size/checksum does not match")
	ErrKey            = errKey
)

// ValidationError describes a user-correctable connection field error.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// OperationError deliberately omits raw SDK responses and signed request URLs.
type OperationError struct {
	Stage   string
	Timeout bool
}

func (e *OperationError) Error() string {
	if e.Timeout {
		return "S3 " + e.Stage + " timed out. Please try again."
	}
	return "S3 " + e.Stage + " failed. Check the connection and object permissions."
}
func (e *OperationError) Unwrap() error {
	if e.Timeout {
		return context.DeadlineExceeded
	}
	return ErrUnavailable
}
func operationError(stage string, err error) error {
	if errors.Is(err, context.Canceled) {
		return context.Canceled
	}
	return &OperationError{Stage: stage, Timeout: errors.Is(err, context.DeadlineExceeded)}
}

// SaveRequest never serves as an API response: it contains write-only keys.
type SaveRequest struct {
	Enabled         bool   `json:"enabled"`
	Bucket          string `json:"bucket"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	PathStyle       bool   `json:"path_style"`
	KeyPrefix       string `json:"key_prefix"`
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

// Settings is the credential-redacted configuration returned to administrators.
type Settings struct {
	Enabled               bool   `json:"enabled"`
	Bucket                string `json:"bucket"`
	Region                string `json:"region"`
	Endpoint              string `json:"endpoint"`
	PathStyle             bool   `json:"path_style"`
	KeyPrefix             string `json:"key_prefix"`
	CredentialsConfigured bool   `json:"credentials_configured"`
	MaskedAccessKeyID     string `json:"masked_access_key_id"`
	LocationLocked        bool   `json:"location_locked"`
}

// Service coordinates configuration snapshots, content and durable deletion jobs.
type Service struct {
	mu          sync.RWMutex
	cleanupMu   sync.Mutex
	cleanupWake chan struct{}
	repo        repository.StorageRepository
	docs        repository.DocumentRepository
	cipher      *credentialCipher
	factory     Factory
}

// NewService creates a content service; a nil factory selects the AWS SDK adapter.
func NewService(repo repository.StorageRepository, docs repository.DocumentRepository, keyPath string, factory Factory) *Service {
	if factory == nil {
		factory = newS3Client
	}
	return &Service{repo: repo, docs: docs, cipher: newCredentialCipher(keyPath), factory: factory, cleanupWake: make(chan struct{}, 1)}
}
func (s *Service) settings(ctx context.Context, c *domain.StorageConfig) (*Settings, error) {
	locked, err := s.repo.HasReferences(ctx)
	if err != nil {
		return nil, err
	}
	return &Settings{Enabled: c.Enabled, Bucket: c.Bucket, Region: c.Region, Endpoint: c.Endpoint, PathStyle: c.PathStyle, KeyPrefix: c.KeyPrefix, CredentialsConfigured: len(c.Credentials) > 0, MaskedAccessKeyID: c.MaskedAccessKeyID, LocationLocked: locked}, nil
}

// Settings returns the safe saved configuration and location-lock state.
func (s *Service) Settings(ctx context.Context) (*Settings, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	return s.settings(ctx, c)
}

var bucketPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
var regionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func controlChars(v string) bool { return strings.ContainsFunc(v, unicode.IsControl) }
func candidate(req SaveRequest, old *domain.StorageConfig, required bool) (*domain.StorageConfig, error) {
	c := *old
	c.Enabled = req.Enabled
	c.Bucket = strings.TrimSpace(req.Bucket)
	c.Region = strings.TrimSpace(req.Region)
	c.Endpoint = strings.TrimRight(strings.TrimSpace(req.Endpoint), "/")
	c.PathStyle = req.PathStyle
	c.KeyPrefix = strings.Trim(req.KeyPrefix, "/")
	bad := func(message string) (*domain.StorageConfig, error) { return nil, &ValidationError{message} }
	if (required || c.Bucket != "") && (!bucketPattern.MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") || strings.Contains(c.Bucket, ".-") || strings.Contains(c.Bucket, "-.") || net.ParseIP(c.Bucket) != nil) {
		return bad("Bucket must be a valid S3 bucket name (3–63 lowercase letters, digits, dots or hyphens).")
	}
	if (required || c.Region != "") && !regionPattern.MatchString(c.Region) {
		return bad("Region is required and must contain lowercase letters, digits or hyphens.")
	}
	if c.Endpoint != "" {
		u, err := url.Parse(c.Endpoint)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || controlChars(c.Endpoint) {
			return bad("Endpoint must be an HTTPS URL without credentials, query or fragment.")
		}
	}
	if len(c.KeyPrefix) > 512 || controlChars(c.KeyPrefix) {
		return bad("Key prefix must be at most 512 bytes without control characters.")
	}
	for _, seg := range strings.Split(c.KeyPrefix, "/") {
		if seg == "." || seg == ".." {
			return bad("Key prefix cannot contain . or .. segments.")
		}
	}
	if c.KeyPrefix != "" {
		c.KeyPrefix += "/"
	}
	if (req.AccessKeyID == "") != (req.SecretAccessKey == "") {
		return bad("Enter both Access Key ID and Secret Access Key to replace saved credentials.")
	}
	if req.AccessKeyID != "" && (len(req.AccessKeyID) > 256 || len(req.SecretAccessKey) > 512 || strings.TrimSpace(req.AccessKeyID) == "" || strings.TrimSpace(req.SecretAccessKey) == "" || controlChars(req.AccessKeyID) || controlChars(req.SecretAccessKey)) {
		return bad("The access keys are invalid or too long.")
	}
	if required && req.AccessKeyID == "" && len(old.Credentials) == 0 {
		return bad("Access Key ID and Secret Access Key are required for the first connection.")
	}
	return &c, nil
}
func locationChanged(a, b *domain.StorageConfig) bool {
	return a.Bucket != b.Bucket || a.Region != b.Region || a.Endpoint != b.Endpoint || a.PathStyle != b.PathStyle
}
func (s *Service) credentials(req SaveRequest, c *domain.StorageConfig) (Credentials, error) {
	if req.AccessKeyID != "" {
		if len(c.Credentials) > 0 {
			if _, err := s.cipher.key(false); err != nil {
				return Credentials{}, err
			}
		}
		return Credentials{AccessKeyID: req.AccessKeyID, SecretAccessKey: req.SecretAccessKey}, nil
	}
	return s.cipher.Open(c.Credentials)
}

// Update verifies and atomically saves administrator-submitted settings.
func (s *Service) Update(ctx context.Context, req SaveRequest) (*Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	c, err := candidate(req, old, req.Enabled)
	if err != nil {
		return nil, err
	}
	if locationChanged(old, c) {
		locked, e := s.repo.HasReferences(ctx)
		if e != nil {
			return nil, e
		}
		if locked {
			return nil, ErrLocationLocked
		}
	}
	probe := req.Enabled || req.AccessKeyID != "" || (len(c.Credentials) > 0 && (locationChanged(old, c) || c.KeyPrefix != old.KeyPrefix))
	if probe {
		if _, err = candidate(req, old, true); err != nil {
			return nil, err
		}
		creds, e := s.credentials(req, old)
		if e != nil {
			return nil, e
		}
		if e = s.probe(ctx, c, creds); e != nil {
			return nil, e
		}
		if req.AccessKeyID != "" {
			c.Credentials, e = s.cipher.Seal(creds)
			if e != nil {
				return nil, e
			}
			tail := ""
			if len(req.AccessKeyID) > 4 {
				tail = req.AccessKeyID[len(req.AccessKeyID)-4:]
			}
			c.MaskedAccessKeyID = "••••" + tail
		}
	}
	if err = s.repo.SaveConfig(ctx, c); err != nil {
		return nil, err
	}
	return s.settings(ctx, c)
}

// Test probes the submitted fields without changing saved settings.
func (s *Service) Test(ctx context.Context, req SaveRequest) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	old, err := s.repo.GetConfig(ctx)
	if err != nil {
		return err
	}
	c, err := candidate(req, old, true)
	if err != nil {
		return err
	}
	creds, err := s.credentials(req, old)
	if err != nil {
		return err
	}
	return s.probe(ctx, c, creds)
}
func (s *Service) probe(ctx context.Context, c *domain.StorageConfig, creds Credentials) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := s.factory(c, creds)
	if err != nil {
		return operationError("connection", err)
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	key := c.KeyPrefix + ".connection-test-" + hex.EncodeToString(nonce)
	data := []byte("dd-portal S3 connection test")
	putErr := client.Put(ctx, c.Bucket, key, data, "text/plain")
	var result error
	if putErr != nil {
		result = operationError("test upload", putErr)
	} else {
		got, e := client.Get(ctx, c.Bucket, key, int64(len(data)))
		if e != nil {
			result = operationError("test read", e)
		} else if !bytes.Equal(got, data) {
			result = ErrContent
		}
	}
	// Cancellation must not prevent an attempt to remove an already-created probe.
	cleanCtx, cleanCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cleanCancel()
	deleteErr := client.Delete(cleanCtx, c.Bucket, key)
	if result != nil {
		return result
	}
	if deleteErr != nil {
		return operationError("test cleanup", deleteErr)
	}
	return nil
}
