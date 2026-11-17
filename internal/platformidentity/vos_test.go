package platformidentity

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVOSVerificationRequiresEnabledStableSubject(t *testing.T) {
	for _, example := range []struct {
		name  string
		body  string
		valid bool
	}{
		{"enabled", `{"sub":"stable-user","preferred_username":"renamed","enable":1}`, true},
		{"boolean", `{"sub":"stable-user","enable":true}`, true},
		{"disabled", `{"sub":"stable-user","enable":0}`, false},
		{"missing_status", `{"sub":"stable-user"}`, false},
		{"missing_subject", `{"preferred_username":"alice","enable":1}`, false},
		{"wrapped", `{"data":{"sub":"stable-user","enable":1}}`, false},
		{"oversized", strings.Repeat("x", 65537), false},
	} {
		t.Run(example.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Header.Get("Authorization") != "Bearer user-token" {
					t.Error("missing bearer")
				}
				_, _ = writer.Write([]byte(example.body))
			}))
			defer server.Close()
			identity, err := VerifyVOS(context.Background(), server.URL, "user-token")
			if (err == nil) != example.valid {
				t.Fatalf("valid=%t error=%v", example.valid, err)
			}
			if example.valid && identity.Subject != "stable-user" {
				t.Fatal("stable subject was lost")
			}
		})
	}
}

func TestVOSVerificationRejectsRedirectAndApplicationCredential(t *testing.T) {
	requests := 0
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		_, _ = writer.Write([]byte(`{"sub":"service","enable":1}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := VerifyVOS(context.Background(), redirect.URL, "user-token"); err == nil {
		t.Fatal("redirect accepted")
	}
	serviceToken := "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"application":"app"}`)) + ".signature"
	if _, err := VerifyVOS(context.Background(), target.URL, serviceToken); err == nil {
		t.Fatal("application credential accepted")
	}
	if requests != 0 {
		t.Fatal("credential reached a redirect target or application userinfo")
	}
}
