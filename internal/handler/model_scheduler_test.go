package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type schedulerCatalogTransport func(*http.Request) (*http.Response, error)

func (f schedulerCatalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSchedulerCatalogUsesServerBindingAndReturnsOnlyIDs(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	t.Setenv("HYBRAG_V_ROUTER_API_KEY", "fixture-private-key")
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = schedulerCatalogTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "http://v-scheduler:8080/v1/models" || r.Header.Get("Authorization") != "Bearer fixture-private-key" {
			t.Fatal("incorrect gateway request")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"registered/chat","secret":"hidden"},{"id":"registered/chat"},{"id":"embedding/bge"},{"id":""}]}`))}, nil
	})
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/v1/models/scheduler", nil)
	(&ModelHandler{}).ListSchedulerModels(c)
	body := recorder.Body.String()
	if recorder.Code != 200 || !strings.Contains(body, `"data":["registered/chat","embedding/bge"]`) || strings.Contains(body, "hidden") || strings.Contains(body, "fixture-private-key") {
		t.Fatalf("unexpected catalog: %s", body)
	}
}

func TestSchedulerCatalogDisabledAndUpstreamFailure(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "model-hub")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/v1/models/scheduler", nil)
	(&ModelHandler{}).ListSchedulerModels(c)
	if recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"enabled":false`) {
		t.Fatal("legacy backend enabled")
	}
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	t.Setenv("HYBRAG_V_ROUTER_API_KEY", "fixture-private-key")
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	http.DefaultTransport = schedulerCatalogTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`private upstream failure`))}, nil
	})
	recorder = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/v1/models/scheduler", nil)
	(&ModelHandler{}).ListSchedulerModels(c)
	if recorder.Code != 502 || strings.Contains(recorder.Body.String(), "private upstream") {
		t.Fatal("upstream failure leaked or ignored")
	}
}
