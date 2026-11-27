package docparser

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/models/api"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

const (
	ComputeErrorCodeBusy    = "WAIT_COMPUTE_RESOURCE"
	computeErrorCodeHeader  = "hybrag-compute-error-code"
	computeRetryAfterHeader = "hybrag-compute-retry-after"
)

// ComputeBusyError marks a temporary compute-resource saturation failure such
// as V-Scheduler's HTTP 429 WAIT_COMPUTE_RESOURCE response.
type ComputeBusyError struct {
	Code       string
	RetryAfter time.Duration
	Err        error
}

func (e *ComputeBusyError) Error() string {
	if e == nil {
		return ""
	}
	code := e.Code
	if code == "" {
		code = ComputeErrorCodeBusy
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: %s", code, e.Err.Error())
	}
	return code
}

func (e *ComputeBusyError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ClassifyComputeBusyError checks whether err represents V-Scheduler's
// 429 WAIT_COMPUTE_RESOURCE (either directly over HTTP or propagated via gRPC)
// and returns the normalized busy error and optional Retry-After hint.
func ClassifyComputeBusyError(err error) (*ComputeBusyError, bool) {
	if err == nil {
		return nil, false
	}
	var busy *ComputeBusyError
	if errors.As(err, &busy) {
		return busy, true
	}
	var httpErr *api.HTTPError
	if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusTooManyRequests {
		if strings.Contains(httpErr.Body, ComputeErrorCodeBusy) ||
			strings.Contains(strings.ToUpper(httpErr.Body), ComputeErrorCodeBusy) {
			return &ComputeBusyError{
				Code:       ComputeErrorCodeBusy,
				RetryAfter: httpErr.RetryAfter(),
				Err:        err,
			}, true
		}
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.ResourceExhausted {
		if strings.Contains(strings.ToUpper(st.Message()), ComputeErrorCodeBusy) {
			return &ComputeBusyError{
				Code: ComputeErrorCodeBusy,
				Err:  err,
			}, true
		}
	}
	upper := strings.ToUpper(err.Error())
	if strings.Contains(upper, ComputeErrorCodeBusy) &&
		(strings.Contains(upper, "429") || strings.Contains(upper, "TOO MANY REQUESTS") || strings.Contains(upper, "RESOURCEEXHAUSTED")) {
		return &ComputeBusyError{
			Code: ComputeErrorCodeBusy,
			Err:  err,
		}, true
	}
	return nil, false
}

func busyErrorFromTrailers(err error, trailers metadata.MD) (*ComputeBusyError, bool) {
	if err == nil {
		return nil, false
	}
	var retryAfter time.Duration
	if values := trailers.Get(computeRetryAfterHeader); len(values) > 0 {
		if secs, parseErr := strconv.Atoi(strings.TrimSpace(values[0])); parseErr == nil && secs > 0 {
			retryAfter = time.Duration(secs) * time.Second
		}
	}
	if values := trailers.Get(computeErrorCodeHeader); len(values) > 0 && values[0] == ComputeErrorCodeBusy {
		return &ComputeBusyError{
			Code:       ComputeErrorCodeBusy,
			RetryAfter: retryAfter,
			Err:        err,
		}, true
	}
	if busy, ok := ClassifyComputeBusyError(err); ok {
		if busy.RetryAfter == 0 && retryAfter > 0 {
			busy.RetryAfter = retryAfter
		}
		return busy, true
	}
	return nil, false
}
