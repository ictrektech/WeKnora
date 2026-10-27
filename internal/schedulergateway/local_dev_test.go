package schedulergateway

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestLocalDevGatewayIsolation(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	t.Setenv("HYBRAG_V_ROUTER_URL", "http://127.0.0.1:22888/v1/")
	t.Setenv("HYBRAG_V_ROUTER_API_KEY", "local-inference-key")
	const gateway = "http://127.0.0.1:22888/v1"
	original := &http.Client{Timeout: 42 * time.Second}
	for _, profile := range []string{"", "tc232"} {
		t.Setenv("ICTREK_DEV_PROFILE", profile)
		if CatalogGateway() != Gateway || Client("", gateway, "", original) != original {
			t.Fatal("host gateway override enabled outside the source-dev Scheduler profile")
		}
	}
	t.Setenv("ICTREK_DEV_PROFILE", "scheduler")
	if CatalogGateway() != gateway || trustedGateway(gateway) {
		t.Fatal("host override changed the production binding allowlist")
	}
	if Client("", "http://another-host/v1", "", original) != original {
		t.Fatal("local key applies to an unconfigured host")
	}
	var gotAuth string
	original.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})
	client := Client("saved-model-without-key", gateway, "registered/model", original)
	if client == original || client.Timeout != original.Timeout {
		t.Fatal("local transport missing or caller timeout changed")
	}
	for _, path := range []string{"models", "chat/completions", "embeddings", "rerank"} {
		method := http.MethodPost
		if path == "models" {
			method = http.MethodGet
		}
		req, _ := http.NewRequest(method, gateway+"/"+path, nil)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if gotAuth != "Bearer local-inference-key" || req.Header.Get("Authorization") != "" {
			t.Fatal("local key missing or original request mutated")
		}
	}
	req, _ := http.NewRequest(http.MethodGet, gateway+"/models", nil)
	req.Header.Set("Authorization", "Bearer per-model-key")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if gotAuth != "Bearer per-model-key" {
		t.Fatal("explicit per-model credential was replaced")
	}
	t.Setenv("HYBRAG_V_ROUTER_API_KEY", "")
	req, _ = http.NewRequest(http.MethodGet, gateway+"/models", nil)
	response, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if gotAuth != "" {
		t.Fatal("empty local key sent an invalid Authorization header")
	}
	for _, url := range []string{gateway + "/admin", "http://other-host/v1/models", Gateway + "/models"} {
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		if _, err := client.Do(req); err == nil {
			t.Fatal("local credential accepted a different host or route")
		}
	}
	t.Setenv("ICTREK_DEV_PROFILE", "")
	req, _ = http.NewRequest(http.MethodGet, gateway+"/models", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("cached local transport remained active after leaving source-dev mode")
	}
}
