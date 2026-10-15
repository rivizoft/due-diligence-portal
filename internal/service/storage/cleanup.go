package storage

import (
	"context"
	"log"
	"time"
)

// Cleanup attempts one bounded batch. Failed jobs remain durable with backoff.
func (s *Service) Cleanup(ctx context.Context) {
	if !s.cleanupMu.TryLock() {
		return
	}
	defer s.cleanupMu.Unlock()
	// Durable jobs pin the endpoint until they finish. Only taking the snapshot
	// needs the settings lock; stalled network I/O must not block administrators.
	s.mu.RLock()
	jobs, err := s.repo.DueCleanup(ctx, 10)
	if err != nil || len(jobs) == 0 {
		s.mu.RUnlock()
		return
	}
	c, err := s.repo.GetConfig(ctx)
	s.mu.RUnlock()
	if err != nil {
		return
	}
	client, err := s.client(c)
	if err != nil {
		log.Print("[WARN] S3 cleanup waiting for valid storage credentials")
		return
	}
	batchCtx, batchCancel := context.WithTimeout(ctx, 60*time.Second)
	defer batchCancel()
	for _, j := range jobs {
		if batchCtx.Err() != nil {
			return
		}
		opCtx, cancel := context.WithTimeout(batchCtx, 60*time.Second)
		err = client.Delete(opCtx, j.Location.Bucket, j.Location.Key)
		cancel()
		if err != nil {
			if e := s.repo.RetryCleanup(ctx, j); e != nil {
				log.Print("[WARN] Failed to schedule S3 cleanup retry")
			}
			continue
		}
		if err = s.repo.FinishCleanup(ctx, j.ID); err != nil {
			log.Print("[WARN] Failed to mark S3 cleanup complete")
		}
	}
}

// ScheduleCleanup wakes the worker without tying network cleanup to an HTTP request.
func (s *Service) ScheduleCleanup() {
	select {
	case s.cleanupWake <- struct{}{}:
	default:
	}
}

// RunCleanup processes startup, scheduled and periodic retries until cancellation.
func (s *Service) RunCleanup(ctx context.Context) {
	s.Cleanup(ctx)
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.Cleanup(ctx)
		case <-s.cleanupWake:
			s.Cleanup(ctx)
		}
	}
}
