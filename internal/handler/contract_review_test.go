package handler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/middleware"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type contractReviewHandlerStub struct {
	interfaces.ContractReviewService
	startFn func(context.Context, uint64, string, string) (*types.ContractReview, error)
	getFn   func(context.Context, uint64, string, string) (*types.ContractReview, error)
	openFn  func(context.Context, uint64, string, string) (*types.ContractReview, io.ReadCloser, error)
}

func (s *contractReviewHandlerStub) Start(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	return s.startFn(ctx, tenantID, userID, id)
}

func (s *contractReviewHandlerStub) Get(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, error) {
	return s.getFn(ctx, tenantID, userID, id)
}

func (s *contractReviewHandlerStub) OpenDocument(ctx context.Context, tenantID uint64, userID, id string) (*types.ContractReview, io.ReadCloser, error) {
	return s.openFn(ctx, tenantID, userID, id)
}

func contractReviewHandlerTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.ErrorHandler())
	r.Use(func(c *gin.Context) {
		c.Set(types.UserIDContextKey.String(), "user-1")
		c.Set(types.TenantIDContextKey.String(), uint64(7))
		c.Next()
	})
	return r
}

func TestContractReviewHandlerMapsInvalidStateToBadRequest(t *testing.T) {
	h := NewContractReviewHandler(&contractReviewHandlerStub{
		startFn: func(context.Context, uint64, string, string) (*types.ContractReview, error) {
			return nil, service.ErrContractReviewInvalidState
		},
	})
	r := contractReviewHandlerTestRouter()
	r.POST("/contract-reviews/:id/start", h.Start)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/contract-reviews/review-1/start", nil))

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "not in a valid state")
}

func TestContractReviewHandlerMapsNotFoundAndServesPreview(t *testing.T) {
	missing := NewContractReviewHandler(&contractReviewHandlerStub{
		getFn: func(context.Context, uint64, string, string) (*types.ContractReview, error) {
			return nil, service.ErrContractReviewNotFound
		},
	})
	r := contractReviewHandlerTestRouter()
	r.GET("/contract-reviews/:id", missing.Get)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contract-reviews/missing", nil))
	require.Equal(t, http.StatusNotFound, w.Code)

	review := &types.ContractReview{ID: "review-1", FileName: "agreement.pdf", FileType: ".pdf", MimeType: "application/pdf", UpdatedAt: time.Now()}
	preview := NewContractReviewHandler(&contractReviewHandlerStub{
		openFn: func(context.Context, uint64, string, string) (*types.ContractReview, io.ReadCloser, error) {
			return review, io.NopCloser(bytes.NewReader([]byte("pdf-bytes"))), nil
		},
	})
	r = contractReviewHandlerTestRouter()
	r.GET("/contract-reviews/:id/document/preview", preview.Preview)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contract-reviews/review-1/document/preview", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/pdf", w.Header().Get("Content-Type"))
	require.Equal(t, "pdf-bytes", w.Body.String())
}

func TestContractReviewHandlerServesLocatorWithoutGuessingLegacyOffsets(t *testing.T) {
	review := &types.ContractReview{
		ID:             "review-1",
		SourceRevision: "text-v2:rev-1",
		SourceTextHash: "rev-1",
		Locator:        types.JSON(`{"version":1,"units":[{"unit_id":"page-1","kind":"page","page":1,"source_start":0,"source_end":4}]}`),
	}
	h := NewContractReviewHandler(&contractReviewHandlerStub{
		getFn: func(context.Context, uint64, string, string) (*types.ContractReview, error) {
			return review, nil
		},
	})
	r := contractReviewHandlerTestRouter()
	r.GET("/contract-reviews/:id/document/locator", h.Locator)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/contract-reviews/review-1/document/locator", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"source_revision":"text-v2:rev-1"`)
	require.Contains(t, w.Body.String(), `"unit_id":"page-1"`)
}
