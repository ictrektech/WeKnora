package interfaces

import "context"

// LegalWorkspaceDataService owns the tenant-wide destructive operation exposed
// by the legal workspace settings page. The implementation must keep the
// contract-review and legal-assistant session scopes separate from ordinary
// platform conversations.
type LegalWorkspaceDataService interface {
	DeleteTenantData(context.Context, uint64) error
}
