package schedulergateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureBinding(revision int64, character string) Binding {
	return Binding{ProtocolVersion: 1, BindingRevision: revision, AppID: AppID, URL: Gateway, Token: "vs-app-" + strings.Repeat(character, 64), Scope: "gateway-inference-v1"}
}

func TestBindingIdentityRevisionAndPrivatePersistence(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	identity := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		subject := strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer ")
		json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": map[string]any{"sub": subject, "enable": 1}})
	}))
	defer identity.Close()
	store := &Store{Filename: filepath.Join(t.TempDir(), "binding.json"), UserinfoURL: identity.URL}
	post := func(subject string, binding Binding) *httptest.ResponseRecorder {
		body, _ := json.Marshal(binding)
		request := httptest.NewRequest(http.MethodPost, Endpoint, bytes.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+subject)
		response := httptest.NewRecorder()
		store.ServeHTTP(response, request)
		if strings.Contains(response.Body.String(), binding.Token) {
			t.Fatal("binding token leaked in response")
		}
		return response
	}
	binding := fixtureBinding(1, "a")
	legacy := fixtureBinding(1, "a")
	legacy.URL = "http://v-router:8080/v1"
	if err := store.save(legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := readBinding(store.Filename); err == nil {
		t.Fatal("legacy alias accepted for inference")
	}
	binding.BindingRevision = 2
	for _, subject := range []string{"admin", "com.ictrek.other", ""} {
		if post(subject, binding).Code != 403 {
			t.Fatal("non-Scheduler identity accepted")
		}
	}
	if post("com.ictrek.v-scheduler", binding).Code != 200 || post("com.ictrek.v-scheduler", binding).Code != 200 {
		t.Fatal("valid binding or idempotent retry failed")
	}
	info, _ := os.Stat(store.Filename)
	if info.Mode().Perm() != 0600 {
		t.Fatal("binding file is not private")
	}
	conflict := fixtureBinding(2, "b")
	if post("com.ictrek.v-scheduler", conflict).Code != 409 {
		t.Fatal("same revision changed token accepted")
	}
	rotated := fixtureBinding(3, "b")
	if post("com.ictrek.v-scheduler", rotated).Code != 200 || post("com.ictrek.v-scheduler", binding).Code != 409 {
		t.Fatal("rotation monotonicity failed")
	}
	rotated.URL = "http://untrusted/v1"
	rotated.BindingRevision = 4
	if post("com.ictrek.v-scheduler", rotated).Code != 422 {
		t.Fatal("foreign gateway accepted")
	}
	response := httptest.NewRecorder()
	store.ServeHTTP(response, httptest.NewRequest(http.MethodGet, Endpoint, nil))
	if strings.Contains(response.Body.String(), "vs-app-") || !strings.Contains(response.Body.String(), `"configured":true`) {
		t.Fatal("status is not safe/configured")
	}
	t.Setenv("HYBRAG_MODEL_BACKEND", "model-hub")
	if post("com.ictrek.v-scheduler", fixtureBinding(3, "c")).Code != 409 {
		t.Fatal("legacy backend accepted automatic binding")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestLongLivedTransportReloadsRotationAndRejectsOtherOrigins(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	store := &Store{Filename: filepath.Join(t.TempDir(), "binding.json")}
	if err := store.save(fixtureBinding(1, "a")); err != nil {
		t.Fatal(err)
	}
	observed := []string{}
	wrapper := transport{filename: store.Filename, base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		observed = append(observed, request.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
	})}
	request := httptest.NewRequest(http.MethodPost, Gateway+"/embeddings", strings.NewReader("{}"))
	request.Header.Set("Authorization", "Bearer old-row-key")
	if _, err := wrapper.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if err := store.save(fixtureBinding(2, "b")); err != nil {
		t.Fatal(err)
	}
	if _, err := wrapper.RoundTrip(request); err != nil {
		t.Fatal(err)
	}
	if observed[0] != "Bearer "+fixtureBinding(1, "a").Token || observed[1] != "Bearer "+fixtureBinding(2, "b").Token || request.Header.Get("Authorization") != "Bearer old-row-key" {
		t.Fatal("rotation or caller-header isolation failed")
	}
	for _, url := range []string{"http://untrusted/v1/embeddings", "http://v-router:8080/v1/embeddings", "http://v-router:8080/api/admin", "https://v-router:8080/v1/embeddings"} {
		if _, err := wrapper.RoundTrip(httptest.NewRequest(http.MethodPost, url, nil)); err == nil {
			t.Fatal("credential escaped fixed inference origin")
		}
	}
	if err := os.Chmod(store.Filename, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := wrapper.RoundTrip(request); err == nil {
		t.Fatal("public binding file accepted")
	}
}

func TestCustomModelsAndLegacyCredentialsAreUntouched(t *testing.T) {
	original := &http.Client{}
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	if Client("hybrag-ollama-qwen35-2b-qa", "http://custom/v1", "v-scheduler/ollama-qa/test", original) != original {
		t.Fatal("custom model transport replaced")
	}
	if Client("hybrag-ollama-qwen35-2b-qa", Gateway, "v-scheduler/ollama-qa/test", original) == original {
		t.Fatal("scheduler model did not enable dynamic credentials")
	}
	t.Setenv("HYBRAG_MODEL_BACKEND", "model-hub")
	if Client("hybrag-ollama-qwen35-2b-qa", Gateway, "v-scheduler/ollama-qa/test", original) != original {
		t.Fatal("legacy model transport replaced")
	}
}

func TestRegisteredModelsUseBinding(t *testing.T) {
	t.Setenv("HYBRAG_MODEL_BACKEND", "scheduler")
	original := &http.Client{}
	for _, name := range []string{"upstream/qwen", "v-scheduler/ollama-embedding/bge-m3"} {
		if Client("user-model", Gateway, name, original) == original {
			t.Fatal("registered model did not use app binding")
		}
	}
}

func TestCanonicalAndCompatibilityGatewayOrigins(t *testing.T) {
	for _, origin := range []string{Gateway, "http://v-scheduler-gateway:8080/v1"} {
		binding := fixtureBinding(1, "a")
		binding.URL = origin
		if !binding.valid() {
			t.Fatalf("known origin rejected: %s", origin)
		}
	}
	for _, origin := range []string{
		"http://v-router:8080/v1", "http://other:8080/v1",
		"https://v-scheduler:8080/v1", "http://v-scheduler:8080/v1/else",
	} {
		if trustedGateway(origin) {
			t.Fatalf("untrusted origin accepted: %s", origin)
		}
	}
}
