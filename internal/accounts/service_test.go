package accounts

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

const accountTestPassword = "mobix-account-test-password"

func accountService(t *testing.T) (*Service, *Store) {
	t.Helper()
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	container, err := mongodb.Run(ctx, "mongo:8.0",
		testcontainers.WithEnv(map[string]string{"GLIBC_TUNABLES": "glibc.pthread.rseq=1"}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Error(err)
		}
	})
	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetTimeout(5 * time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	})
	store := NewStore(client.Database("accounts_test"))
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	// Keep the Mongo fixture focused on account behavior. The gateway integration
	// test exercises the production hash cost through HTTP.
	service, err := newService(store, bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func TestAccountsProvisionLoginAndSessionLifecycle(t *testing.T) {
	service, store := accountService(t)
	ctx := context.Background()
	operatorEmail := "operator@example.test"

	t.Run("bootstrap requires initial credentials", func(t *testing.T) {
		if err := service.Bootstrap(ctx, "", ""); err == nil {
			t.Fatal("bootstrap accepted missing credentials")
		}
	})

	t.Run("concurrent bootstrap creates one operator", func(t *testing.T) {
		var workers sync.WaitGroup
		for range 2 {
			workers.Go(func() {
				if err := service.Bootstrap(ctx, operatorEmail, accountTestPassword); err != nil {
					t.Error(err)
				}
			})
		}
		workers.Wait()
		count, err := store.users.CountDocuments(ctx, bson.M{"role": authn.Operator})
		if err != nil || count != 1 {
			t.Fatalf("operator count=%d error=%v, want one operator", count, err)
		}
		before, err := store.credential(ctx, operatorEmail)
		if err != nil {
			t.Fatal(err)
		}
		if err := service.Bootstrap(ctx, "replacement@example.test", "different-test-password"); err != nil {
			t.Fatal(err)
		}
		after, err := store.credential(ctx, operatorEmail)
		if err != nil || !bytes.Equal(before.PasswordHash, after.PasswordHash) || before.Account != after.Account {
			t.Fatalf("bootstrap changed an existing account: error=%v", err)
		}
		if _, err := store.credential(ctx, "replacement@example.test"); !errors.Is(err, errUnknown) {
			t.Fatalf("bootstrap unexpectedly provisioned another account: %v", err)
		}
	})

	operator, err := service.Login(ctx, strings.ToUpper(operatorEmail), accountTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	operatorCtx := authn.WithActor(ctx, operator.Account.Actor())
	accountsByRole := make(map[authn.Role]Account)
	logins := make(map[authn.Role]LoginResult)
	for _, role := range []authn.Role{authn.Rider, authn.Driver, authn.Operator} {
		t.Run("provision and sign in "+string(role), func(t *testing.T) {
			email := string(role) + "-member@example.test"
			account, err := service.Create(operatorCtx, "  "+strings.ToUpper(email)+"  ", accountTestPassword, role)
			if err != nil {
				t.Fatal(err)
			}
			if account.Email != email || account.Role != role || account.ID == "" {
				t.Fatalf("incorrect provisioned account: %+v", account)
			}
			credential, err := store.credential(ctx, email)
			if err != nil || bytes.Equal(credential.PasswordHash, []byte(accountTestPassword)) ||
				bcrypt.CompareHashAndPassword(credential.PasswordHash, []byte(accountTestPassword)) != nil {
				t.Fatalf("password was not securely stored: error=%v", err)
			}
			login, err := service.Login(ctx, " "+strings.ToUpper(email)+" ", accountTestPassword)
			if err != nil {
				t.Fatal(err)
			}
			if login.Account != account || login.TokenType != "Bearer" ||
				!login.ExpiresAt.After(time.Now().Add(11*time.Hour)) || login.ExpiresAt.After(time.Now().Add(12*time.Hour+time.Second)) {
				t.Fatalf("incorrect login metadata: %+v", login.Account)
			}
			actual, err := service.Authenticate(ctx, login.Token)
			if err != nil || actual != account {
				t.Fatalf("new session was not usable: account=%+v error=%v", actual, err)
			}
			accountsByRole[role] = account
			logins[role] = login
		})
	}
	if len(logins) != 3 {
		t.Fatal("role provisioning failed")
	}

	t.Run("duplicate normalized email conflicts", func(t *testing.T) {
		_, err := service.Create(operatorCtx, "  RIDER-MEMBER@EXAMPLE.TEST ", accountTestPassword, authn.Rider)
		if !errors.Is(err, apperrors.ErrAlreadyExists) {
			t.Fatalf("duplicate normalized email returned %v", err)
		}
	})

	t.Run("non operators cannot provision accounts", func(t *testing.T) {
		for _, actor := range []authn.Actor{{}, accountsByRole[authn.Rider].Actor(), accountsByRole[authn.Driver].Actor(), {Role: authn.Service}} {
			_, err := service.Create(authn.WithActor(ctx, actor), "blocked@example.test", accountTestPassword, authn.Operator)
			if !errors.Is(err, apperrors.ErrForbidden) {
				t.Fatalf("actor %+v was allowed to provision: %v", actor, err)
			}
		}
		if _, err := store.credential(ctx, "blocked@example.test"); !errors.Is(err, errUnknown) {
			t.Fatalf("forbidden request wrote an account: %v", err)
		}
		if _, err := service.Create(operatorCtx, "service@example.test", accountTestPassword, authn.Service); !errors.Is(err, apperrors.ErrInvalidInput) {
			t.Fatalf("accepted a service role account: %v", err)
		}
	})

	t.Run("unknown and wrong login return the same public error", func(t *testing.T) {
		for _, email := range []string{accountsByRole[authn.Rider].Email, "unknown@example.test"} {
			result, err := service.Login(ctx, email, "incorrect-test-password")
			if !errors.Is(err, apperrors.ErrUnauthorized) || result.Token != "" {
				t.Fatalf("login %q exposed an account distinction or token: error=%v", email, err)
			}
		}
	})

	t.Run("sessions store only a digest of the bearer token", func(t *testing.T) {
		login := logins[authn.Rider]
		digest := sha256.Sum256([]byte(login.Token))
		var document bson.M
		if err := store.sessions.FindOne(ctx, bson.M{"_id": hex.EncodeToString(digest[:])}).Decode(&document); err != nil {
			t.Fatal(err)
		}
		encoded, err := bson.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(encoded, []byte(login.Token)) || bytes.Contains(encoded, []byte(accountTestPassword)) || bytes.Contains(encoded, []byte("password_hash")) {
			t.Fatal("a session document contains bearer or password credentials")
		}
		if err := store.sessions.FindOne(ctx, bson.M{"_id": login.Token}).Err(); !errors.Is(err, mongo.ErrNoDocuments) {
			t.Fatalf("raw bearer token is stored as a session key: %v", err)
		}
	})

	t.Run("expiry is enforced before TTL cleanup", func(t *testing.T) {
		login := logins[authn.Driver]
		digest := sha256.Sum256([]byte(login.Token))
		hash := hex.EncodeToString(digest[:])
		if _, err := store.activeSession(ctx, hash, login.ExpiresAt); !errors.Is(err, errUnknown) {
			t.Fatalf("a session is accepted at its exact expiry: %v", err)
		}
		if err := store.sessions.FindOne(ctx, bson.M{"_id": hash}).Err(); err != nil {
			t.Fatalf("future-expiring session was removed during the expiry check: %v", err)
		}
		if _, err := store.sessions.UpdateOne(ctx, bson.M{"_id": hash}, bson.M{"$set": bson.M{"expires_at": time.Now().Add(-time.Second)}}); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(ctx, login.Token); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("expired bearer token was accepted: %v", err)
		}
	})

	t.Run("logout immediately revokes only its own session", func(t *testing.T) {
		login := logins[authn.Rider]
		if err := service.Logout(ctx, login.Token); err != nil {
			t.Fatal(err)
		}
		if _, err := service.Authenticate(ctx, login.Token); !errors.Is(err, apperrors.ErrUnauthorized) {
			t.Fatalf("logged out token was accepted: %v", err)
		}
		if _, err := service.Authenticate(ctx, operator.Token); err != nil {
			t.Fatalf("logout revoked another account's session: %v", err)
		}
		if err := service.Logout(ctx, login.Token); err != nil {
			t.Fatalf("repeated logout failed: %v", err)
		}
	})

	t.Run("HTTP provisioning requires an operator session", func(t *testing.T) {
		mux := http.NewServeMux()
		service.RegisterHTTP(mux)
		body := `{"email":"elevated@example.test","password":"another-test-password","role":"operator"}`
		request := httptest.NewRequest(http.MethodPost, "/v1/accounts", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+logins[authn.Operator].Token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("operator provisioning failed: %d %s", response.Code, response.Body)
		}
		var created map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if created["role"] != string(authn.Operator) || created["password"] != nil || created["password_hash"] != nil {
			t.Fatalf("unexpected public account fields: %v", created)
		}

		loginBody, err := json.Marshal(map[string]string{
			"email": accountsByRole[authn.Rider].Email, "password": accountTestPassword,
		})
		if err != nil {
			t.Fatal(err)
		}
		request = httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewReader(loginBody))
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("HTTP login failed: %d %s", response.Code, response.Body)
		}
		var riderLogin LoginResult
		if err := json.Unmarshal(response.Body.Bytes(), &riderLogin); err != nil {
			t.Fatal(err)
		}
		request = httptest.NewRequest(http.MethodPost, "/v1/accounts", strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+riderLogin.Token)
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("rider provisioned an operator: %d %s", response.Code, response.Body)
		}
		request = httptest.NewRequest(http.MethodPost, "/v1/accounts", strings.NewReader(body))
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("anonymous provisioning returned %d without a bearer challenge", response.Code)
		}

		request = httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
		request.Header.Set("Authorization", "Bearer "+riderLogin.Token)
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		var current Account
		if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
			t.Fatal(err)
		}
		if response.Code != http.StatusOK || current != accountsByRole[authn.Rider] {
			t.Fatalf("me returned the wrong account: status=%d account=%+v", response.Code, current)
		}
		request = httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
		request.Header.Set("Authorization", "Bearer "+riderLogin.Token)
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("HTTP logout failed: %d %s", response.Code, response.Body)
		}
		request = httptest.NewRequest(http.MethodGet, "/v1/auth/me", nil)
		request.Header.Set("Authorization", "Bearer "+riderLogin.Token)
		response = httptest.NewRecorder()
		mux.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("logged out HTTP bearer is still authorized: %d", response.Code)
		}
	})
}
