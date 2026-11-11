package authn

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

const IdentityHeader = "X-Mobix-Identity"

const assertionTTL = 30 * time.Second

type Delegation struct{ key []byte }

func LoadDelegation() (Delegation, error) {
	key := os.Getenv("MOBIX_INTERNAL_KEY")
	if len(key) < 32 {
		return Delegation{}, fmt.Errorf("MOBIX_INTERNAL_KEY must contain at least 32 bytes")
	}
	return Delegation{key: []byte(key)}, nil
}

type assertion struct {
	Actor
	Audience string `json:"aud"`
	Method   string `json:"method"`
	Path     string `json:"path"`
	Expiry   int64  `json:"exp"`
}

func (d Delegation) Sign(r *http.Request, audience string, actor Actor) {
	data, _ := json.Marshal(assertion{
		Actor: actor, Audience: audience, Method: r.Method, Path: r.URL.EscapedPath(),
		Expiry: time.Now().Add(assertionTTL).Unix(),
	})
	payload := base64.RawURLEncoding.EncodeToString(data)
	r.Header.Set(IdentityHeader, payload+"."+base64.RawURLEncoding.EncodeToString(d.signature(payload)))
}

func (d Delegation) signature(payload string) []byte {
	mac := hmac.New(sha256.New, d.key)
	_, _ = mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (d Delegation) verify(r *http.Request, audience string, now time.Time) (Actor, bool) {
	headers := r.Header.Values(IdentityHeader)
	if len(d.key) < 32 || len(headers) != 1 || len(headers[0]) > 2048 {
		return Actor{}, false
	}
	payload, signature, ok := strings.Cut(headers[0], ".")
	signed, err := base64.RawURLEncoding.DecodeString(signature)
	if !ok || err != nil || !hmac.Equal(signed, d.signature(payload)) {
		return Actor{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(payload)
	var claims assertion
	if err != nil || json.Unmarshal(data, &claims) != nil ||
		claims.Audience != audience || claims.Method != r.Method || claims.Path != r.URL.EscapedPath() ||
		claims.Expiry <= now.Unix() || claims.Expiry > now.Add(assertionTTL+5*time.Second).Unix() {
		return Actor{}, false
	}
	switch claims.Role {
	case Rider, Driver, Operator:
		return claims.Actor, claims.ID != ""
	case Service:
		return claims.Actor, claims.ID == ""
	default:
		return Actor{}, false
	}
}

func (d Delegation) Protect(audience string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			(r.URL.EscapedPath() == "/health" || r.URL.EscapedPath() == "/ready") {
			next.ServeHTTP(w, r)
			return
		}
		actor, ok := d.verify(r, audience, time.Now())
		if !ok {
			httpapi.Error(w, apperrors.ErrUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(WithActor(r.Context(), actor)))
	})
}
