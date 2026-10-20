package schedulergateway

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const AppID = "com.ictrek.hybrag"
const Endpoint = "/api/v1/scheduler/gateway-binding"

// Gateway is the primary unified V-Scheduler inference origin.
const Gateway = "http://v-scheduler:8080/v1"

// 仅允许统一容器及其固定兼容别名，保留已有绑定和模型配置。
func trustedGateway(value string) bool {
	return value == Gateway || value == "http://v-scheduler-gateway:8080/v1"
}

var bindingMutex sync.Mutex
var tokenPattern = regexp.MustCompile(`^vs-app-[a-f0-9]{64}$`)

type Binding struct {
	ProtocolVersion int    `json:"protocol_version"`
	BindingRevision int64  `json:"binding_revision"`
	AppID           string `json:"app_id"`
	URL             string `json:"url"`
	Token           string `json:"token"`
	Scope           string `json:"scope"`
}

func Backend() string {
	backend := os.Getenv("HYBRAG_MODEL_BACKEND")
	if backend == "auto" {
		if _, err := net.LookupHost("v-scheduler"); err == nil {
			return "scheduler"
		}
	}
	if backend == "scheduler" {
		return backend
	}
	return "model-hub"
}

func bindingPath() string {
	return "/data/scheduler-gateway.local.json"
}

func (binding Binding) valid() bool {
	return binding.ProtocolVersion == 1 && binding.BindingRevision > 0 && binding.AppID == AppID &&
		trustedGateway(binding.URL) && binding.Scope == "gateway-inference-v1" &&
		tokenPattern.MatchString(binding.Token)
}

func readBinding(filename string) (Binding, error) {
	return readStoredBinding(filename, false)
}

func readStoredBinding(filename string, forRebinding bool) (Binding, error) {
	var binding Binding
	info, err := os.Lstat(filename)
	if err != nil {
		return binding, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 16384 {
		return binding, errors.New("private gateway binding required")
	}
	file, err := os.Open(filename)
	if err != nil {
		return binding, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 16385))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&binding) != nil {
		return Binding{}, errors.New("invalid gateway binding")
	}
	validation := binding
	if forRebinding && validation.URL == "http://v-router:8080/v1" {
		validation.URL = Gateway
	}
	if !validation.valid() {
		return Binding{}, errors.New("invalid gateway binding")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return Binding{}, errors.New("invalid gateway binding")
	}
	return binding, nil
}

type Store struct {
	Filename    string
	UserinfoURL string
}

func Handler() http.Handler {
	endpoint := os.Getenv("HYBRAG_VOS_OIDC_USERINFO_URL")
	if endpoint == "" {
		endpoint = os.Getenv("WEKNORA_VOS_OIDC_USERINFO_URL")
	}
	if endpoint == "" {
		endpoint = "http://172.17.0.1:8105/v1000/oauth2/userinfo"
	}
	return &Store{Filename: bindingPath(), UserinfoURL: endpoint}
}

func (store *Store) authorized(request *http.Request) bool {
	header := request.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") || len(header) > 8192 {
		return false
	}
	check, err := http.NewRequestWithContext(request.Context(), http.MethodGet, store.UserinfoURL, nil)
	if err != nil {
		return false
	}
	check.Header.Set("Authorization", header)
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(check)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var claims map[string]any
	if json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&claims) != nil {
		return false
	}
	if code, exists := claims["code"]; exists && code != float64(0) {
		return false
	}
	if data, ok := claims["data"].(map[string]any); ok {
		claims = data
	}
	return claims["sub"] == "com.ictrek.v-scheduler" && claims["enable"] == float64(1)
}

func (store *Store) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "application/json")
	writer.Header().Set("Cache-Control", "no-store")
	if request.Method == http.MethodGet {
		binding, err := readBinding(store.Filename)
		_ = json.NewEncoder(writer).Encode(map[string]any{
			"protocol_version": 1, "app_id": AppID,
			"capabilities":     []string{"gateway-inference-v1", "canonical-v-scheduler-v1"},
			"binding_endpoint": Endpoint, "backend": Backend(),
			"configured":       err == nil && Backend() == "scheduler",
			"binding_revision": binding.BindingRevision,
		})
		return
	}
	if request.Method != http.MethodPost {
		writer.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !store.authorized(request) {
		writer.WriteHeader(http.StatusForbidden)
		return
	}
	if Backend() != "scheduler" {
		writer.WriteHeader(http.StatusConflict)
		return
	}
	var binding Binding
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 16384))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&binding) != nil || !binding.valid() || decoder.Decode(new(any)) != io.EOF {
		writer.WriteHeader(http.StatusUnprocessableEntity)
		return
	}
	bindingMutex.Lock()
	defer bindingMutex.Unlock()
	old, err := readStoredBinding(store.Filename, true)
	if err == nil && old.BindingRevision >= binding.BindingRevision {
		if old != binding {
			writer.WriteHeader(http.StatusConflict)
			return
		}
	} else {
		if err != nil && !os.IsNotExist(err) {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if err = store.save(binding); err != nil {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
	}
	json.NewEncoder(writer).Encode(map[string]any{"protocol_version": 1, "binding_revision": binding.BindingRevision, "configured": true})
}

func (store *Store) save(binding Binding) error {
	if err := os.MkdirAll(filepath.Dir(store.Filename), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(store.Filename), ".scheduler-gateway-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = json.NewEncoder(file).Encode(binding); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), store.Filename)
}
