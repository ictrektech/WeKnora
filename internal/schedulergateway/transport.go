package schedulergateway

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

type transport struct {
	base         http.RoundTripper
	filename     string
	localGateway string
}

// CatalogGateway keeps host overrides confined to the explicit source-dev profile.
func CatalogGateway() string {
	if os.Getenv("ICTREK_DEV_PROFILE") != "scheduler" {
		return Gateway
	}
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("HYBRAG_V_ROUTER_URL")), "/")
	if value == "" {
		return Gateway
	}
	if !strings.HasSuffix(value, "/v1") {
		value += "/v1"
	}
	return value
}

func (wrapper transport) RoundTrip(request *http.Request) (*http.Response, error) {
	allowed := request.Method == http.MethodGet && request.URL.Path == "/v1/models" || request.Method == http.MethodPost &&
		(request.URL.Path == "/v1/chat/completions" || request.URL.Path == "/v1/embeddings" || request.URL.Path == "/v1/rerank")
	if wrapper.localGateway != "" {
		if Backend() != "scheduler" || os.Getenv("ICTREK_DEV_PROFILE") != "scheduler" || !allowed ||
			request.URL.Scheme+"://"+request.URL.Host+"/v1" != wrapper.localGateway || CatalogGateway() != wrapper.localGateway {
			return nil, errors.New("local gateway credential is limited to configured inference routes")
		}
		copy := request.Clone(request.Context())
		if key := os.Getenv("HYBRAG_V_ROUTER_API_KEY"); key != "" && copy.Header.Get("Authorization") == "" {
			copy.Header.Set("Authorization", "Bearer "+key)
		}
		return wrapper.base.RoundTrip(copy)
	}
	if Backend() != "scheduler" || request.URL.Scheme != "http" ||
		!trustedGateway("http://"+request.URL.Host+"/v1") || !allowed {
		return nil, errors.New("gateway credential is limited to fixed inference routes")
	}
	key := ""
	binding, err := readBinding(wrapper.filename)
	if err == nil {
		key = binding.Token
	} else if os.IsNotExist(err) {
		key = os.Getenv("HYBRAG_V_ROUTER_API_KEY")
	}
	if key == "" {
		return nil, errors.New("Scheduler gateway binding required")
	}
	copy := request.Clone(request.Context())
	copy.Header.Set("Authorization", "Bearer "+key)
	return wrapper.base.RoundTrip(copy)
}

func Client(modelID, baseURL, remoteModel string, original *http.Client) *http.Client {
	baseURL = strings.TrimRight(baseURL, "/")
	localGateway := ""
	if os.Getenv("ICTREK_DEV_PROFILE") == "scheduler" && baseURL == CatalogGateway() && !trustedGateway(baseURL) {
		localGateway = baseURL
	}
	if Backend() != "scheduler" || (!trustedGateway(baseURL) && localGateway == "") {
		return original
	}
	client := http.Client{}
	if original != nil {
		client = *original
	}
	base := client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	client.Transport = transport{base: base, filename: bindingPath(), localGateway: localGateway}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}

// InferenceRequestTimeout includes bounded Scheduler queueing and cold loading.
// Explicit model timeouts and caller context cancellation remain authoritative.
func InferenceRequestTimeout(baseURL string, configured, fallback time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	if Backend() == "scheduler" && trustedGateway(strings.TrimRight(baseURL, "/")) {
		return 180 * time.Second
	}
	return fallback
}
