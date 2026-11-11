package authn

import (
	"net/http/httptest"
	"testing"
)

func TestBearerRejectsAmbiguousCredentials(t *testing.T) {
	tests := []struct {
		name    string
		headers []string
		want    string
	}{
		{name: "standard", headers: []string{"Bearer token"}, want: "token"},
		{name: "case insensitive scheme", headers: []string{"bearer token"}, want: "token"},
		{name: "missing"},
		{name: "wrong scheme", headers: []string{"Basic token"}},
		{name: "extra field", headers: []string{"Bearer token extra"}},
		{name: "combined credentials", headers: []string{"Bearer token,Bearer another"}},
		{name: "tab in token", headers: []string{"Bearer token\tvalue"}},
		{name: "duplicate headers", headers: []string{"Bearer token", "Bearer another"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/v1/auth/me", nil)
			for _, header := range test.headers {
				request.Header.Add("Authorization", header)
			}
			if got := Bearer(request); got != test.want {
				t.Fatalf("got token %q, want %q", got, test.want)
			}
		})
	}
}
