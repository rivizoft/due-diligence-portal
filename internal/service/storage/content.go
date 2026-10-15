package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"time"

	"github.com/rivizoft/due-diligence-portal/internal/domain"
)

func (s *Service) client(c *domain.StorageConfig) (Client, error) {
	creds, err := s.cipher.Open(c.Credentials)
	if err != nil {
		return nil, err
	}
	client, err := s.factory(c, creds)
	if err != nil {
		return nil, operationError("connection", err)
	}
	return client, nil
}

// Store holds a stable configuration until the metadata transaction has committed.
func (s *Service) Store(ctx context.Context, v *domain.DocumentVersion, persist func() error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, err := s.repo.GetConfig(ctx)
	if err != nil {
		return err
	}
	if !c.Enabled {
		return persist()
	}
	client, err := s.client(c)
	if err != nil {
		return err
	}
	opCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	location := domain.ObjectLocation{Bucket: c.Bucket, Key: c.KeyPrefix + v.DocumentID + "/" + v.ID}
	if err = client.Put(opCtx, location.Bucket, location.Key, v.FileData, v.MimeType); err != nil {
		s.compensate(ctx, client, location)
		return operationError("upload", err)
	}
	data := v.FileData
	v.FileData = []byte{}
	v.Storage = &location
	if err = persist(); err != nil {
		v.FileData = data
		v.Storage = nil
		s.compensate(ctx, client, location)
		return err
	}
	return nil
}
func (s *Service) compensate(ctx context.Context, client Client, location domain.ObjectLocation) {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := client.Delete(cleanupCtx, location.Bucket, location.Key); err != nil {
		// Delete may have exhausted its deadline; persist the retry independently.
		queueCtx, queueCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer queueCancel()
		if queueErr := s.repo.QueueCleanup(queueCtx, location); queueErr != nil {
			log.Print("[WARN] S3 compensating deletion could not be queued; an orphan object may remain")
		}
	}
}

// Read fetches content only when called by an authorized delivery/PDF path.
func (s *Service) Read(ctx context.Context, documentID string, number int) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, err := s.docs.GetVersion(ctx, documentID, number)
	if err != nil {
		return nil, err
	}
	if v.Storage == nil {
		return v.FileData, nil
	}
	c, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	client, err := s.client(c)
	if err != nil {
		return nil, err
	}
	opCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	data, err := client.Get(opCtx, v.Storage.Bucket, v.Storage.Key, v.FileSize)
	if err != nil {
		if errors.Is(err, ErrContent) {
			return nil, ErrContent
		}
		return nil, operationError("read", err)
	}
	checksum := sha256.Sum256(data)
	if int64(len(data)) != v.FileSize || hex.EncodeToString(checksum[:]) != v.ChecksumSHA256 {
		return nil, ErrContent
	}
	return data, nil
}

// ReadPrefix is used only for authorized MIME repair; it never loads the whole
// original or replaces the integrity-checked Read used for binary delivery.
func (s *Service) ReadPrefix(ctx context.Context, documentID string, number int) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, err := s.docs.GetVersionMetadata(ctx, documentID, number)
	if err != nil {
		return nil, err
	}
	if v.Storage == nil {
		return s.docs.GetVersionPrefix(ctx, documentID, number)
	}
	if v.FileSize == 0 {
		return []byte{}, nil
	}
	c, err := s.repo.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	client, err := s.client(c)
	if err != nil {
		return nil, err
	}
	opCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	data, err := client.GetPrefix(opCtx, v.Storage.Bucket, v.Storage.Key, min(v.FileSize, 1024))
	if err != nil {
		return nil, operationError("read", err)
	}
	return data, nil
}
