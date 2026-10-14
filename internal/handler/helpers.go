package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/witfoo/due-diligence-portal/internal/domain"
	"github.com/witfoo/due-diligence-portal/internal/repository"
	"github.com/witfoo/due-diligence-portal/pkg/envconfig"
)

// investorAccessMode controls whether investors need explicit access grants.
// "all" (default): every authenticated investor sees every document — login and
// the NDA gate are the only barriers. "granted": investors see only documents
// covered by an access grant (per-document or per-category).
var investorAccessMode = envconfig.GetEnv("DD_INVESTOR_ACCESS", "all")

// investorAccessOpen reports whether ungranted investors may access documents.
func investorAccessOpen() bool { return investorAccessMode != "granted" }

// generateHandlerID generates a random hex ID for new resources.
func generateHandlerID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// documentAccessAllowed reports whether a user with the given role may access doc
// at the required level. Staff (admin/company member) always may. Investors may
// with open access unless explicit grants constrain the requested level; in "granted" mode they need a
// matching grant on the document OR on its category. This is the single source
// of truth for per-document authorization across handlers.
func documentAccessAllowed(ctx context.Context, permRepo repository.PermissionRepository, userID, role string, doc *domain.Document, level string) (bool, error) {
	if doc.IsArchived {
		return false, nil
	}
	if role == domain.RoleAdmin || role == domain.RoleCompanyMember {
		return true, nil
	}
	// Explicit grants constrain downloads even when ungranted investors have open access.
	// In granted mode retain the existing additive document/category grant behavior.
	if !investorAccessOpen() {
		if ok, err := permRepo.HasAccess(ctx, userID, domain.ResourceDocument, doc.ID, level); err != nil {
			return false, err
		} else if ok {
			return true, nil
		}
		return permRepo.HasAccess(ctx, userID, domain.ResourceCategory, doc.CategoryID, level)
	}
	grants, err := permRepo.ListByUser(ctx, userID)
	if err != nil {
		return false, err
	}
	matched, allowed := false, false
	for _, grant := range grants {
		if (grant.ResourceType == domain.ResourceDocument && grant.ResourceID == doc.ID) ||
			(grant.ResourceType == domain.ResourceCategory && grant.ResourceID == doc.CategoryID) {
			matched = true
			if (grant.ExpiresAt == nil || grant.ExpiresAt.After(time.Now())) && domain.HasAccess(grant.AccessLevel, level) {
				allowed = true
			}
		}
	}
	if matched {
		return allowed, nil
	}
	return true, nil
}
