package ratelimit

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
)

var (
	errLimited     = apperrors.New(http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests. Try again later")
	errUnavailable = apperrors.New(http.StatusServiceUnavailable, "RATE_LIMIT_UNAVAILABLE", "Request protection is temporarily unavailable")
)

type failureMode string

const (
	failOpen   failureMode = "open"
	failClosed failureMode = "closed"
)

type Limiter struct {
	backend  Backend
	secret   []byte
	resolver IPResolver
	policies map[Operation]Policy
	logMu    sync.Mutex
	lastLog  map[Operation]time.Time
}

func New(backend Backend, config Config) (*Limiter, error) {
	if backend == nil {
		return nil, fmt.Errorf("rate limit backend is required")
	}
	if len(config.KeySecret) < 32 {
		return nil, fmt.Errorf("rate limit key secret must be at least 32 bytes")
	}
	resolver, err := NewIPResolver(config.TrustedProxyCIDR)
	if err != nil {
		return nil, err
	}
	for _, operation := range []Operation{LoginIP, LoginAccount, RideRequest, Location, OfferPoll, OfferResponse, EventStream} {
		policy, ok := config.Policies[operation]
		if !ok || policy.Capacity < 1 || policy.Window < time.Second {
			return nil, fmt.Errorf("invalid or missing policy %q", operation)
		}
	}
	return &Limiter{
		backend: backend, secret: []byte(config.KeySecret), resolver: resolver,
		policies: config.Policies, lastLog: make(map[Operation]time.Time),
	}, nil
}

func (limiter *Limiter) Login(next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}
	account := limiter.protect(LoginAccount, failClosed, loginAccountSubject, next)
	return limiter.protect(LoginIP, failClosed, limiter.ipSubject, account)
}

func (limiter *Limiter) Account(operation Operation, next http.Handler) http.Handler {
	if limiter == nil {
		return next
	}
	return limiter.protect(operation, failOpen, actorSubject, next)
}

type subjectFunc func(*http.Request) (string, bool)

func (limiter *Limiter) protect(operation Operation, mode failureMode, subject subjectFunc, next http.Handler) http.Handler {
	policy := limiter.policies[operation]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		value, ok := subject(r)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		decision, err := limiter.backend.Allow(r.Context(), limiter.key(operation, value), policy)
		if err != nil {
			limiter.logFailure(operation, mode, err)
			if mode == failClosed {
				httpapi.Error(w, errUnavailable)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if !decision.Allowed {
			seconds := max(int64(1), (decision.RetryAfter.Milliseconds()+999)/1000)
			w.Header().Set("Retry-After", fmt.Sprintf("%d", seconds))
			httpapi.Error(w, errLimited)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (limiter *Limiter) logFailure(operation Operation, mode failureMode, err error) {
	now := time.Now()
	limiter.logMu.Lock()
	last := limiter.lastLog[operation]
	shouldLog := last.IsZero() || now.Sub(last) >= 5*time.Second
	if shouldLog {
		limiter.lastLog[operation] = now
	}
	limiter.logMu.Unlock()
	if shouldLog {
		slog.Error("rate limit backend unavailable", "policy", operation, "failure_mode", mode, "error", err)
	}
}

func (limiter *Limiter) key(operation Operation, subject string) string {
	digest := hmac.New(sha256.New, limiter.secret)
	digest.Write([]byte(subject))
	return "mobix:rate:v1:" + string(operation) + ":" + hex.EncodeToString(digest.Sum(nil))
}

func (limiter *Limiter) ipSubject(request *http.Request) (string, bool) {
	return limiter.resolver.ClientIP(request)
}

func actorSubject(request *http.Request) (string, bool) {
	actor := authn.From(request.Context())
	return actor.ID, actor.ID != ""
}

func loginAccountSubject(request *http.Request) (string, bool) {
	if request.Body == nil {
		return "", false
	}
	data, err := io.ReadAll(io.LimitReader(request.Body, httpapi.MaxBody+1))
	request.Body.Close()
	request.Body = io.NopCloser(bytes.NewReader(data))
	if err != nil || len(data) > httpapi.MaxBody {
		return "", false
	}
	var body struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(data, &body) != nil {
		return "", false
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	return email, email != ""
}
