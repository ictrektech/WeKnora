package interfaces

import "context"

type PatentWorkspaceDataService interface {
	DeleteTenantData(context.Context, uint64) error
}
