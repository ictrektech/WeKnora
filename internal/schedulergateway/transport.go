package schedulergateway

import (
	"errors"
	"net/http"
	"os"
	"strings"
)

type transport struct {
	base     http.RoundTripper
	filename string
}

func (wrapper transport) RoundTrip(request *http.Request) (*http.Response, error) {
	allowed := request.Method == http.MethodGet && request.URL.Path == "/v1/models" || request.Method == http.MethodPost &&
		(request.URL.Path == "/v1/chat/completions" || request.URL.Path == "/v1/embeddings" || request.URL.Path == "/v1/rerank")
	if Backend() != "scheduler" || request.URL.Scheme != "http" || request.URL.Host != "v-scheduler-gateway:8080" || !allowed {
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
	if Backend() != "scheduler" || strings.TrimRight(baseURL, "/") != Gateway {
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
	client.Transport = transport{base: base, filename: bindingPath()}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
