package handler_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/internal/accounts"
	driverhandler "github.com/bytepharaoh/Mobix/internal/driver/handler"
	driverrepo "github.com/bytepharaoh/Mobix/internal/driver/repository"
	driverservice "github.com/bytepharaoh/Mobix/internal/driver/service"
	gateway "github.com/bytepharaoh/Mobix/internal/gateway/handler"
	paymenthandler "github.com/bytepharaoh/Mobix/internal/payment/handler"
	paymentrepo "github.com/bytepharaoh/Mobix/internal/payment/repository"
	tripdomain "github.com/bytepharaoh/Mobix/internal/trip/domain"
	"github.com/bytepharaoh/Mobix/internal/trip/driverclient"
	triphandler "github.com/bytepharaoh/Mobix/internal/trip/handler"
	triprepo "github.com/bytepharaoh/Mobix/internal/trip/repository"
	tripservice "github.com/bytepharaoh/Mobix/internal/trip/service"
	"github.com/bytepharaoh/Mobix/pkg/authn"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/mongodb"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

// This suite exercises account sessions, gateway delegation, and resource access
// through the real HTTP handlers. The runtime smoke suite covers RabbitMQ delivery.
func TestAuthenticatedRideAccess(t *testing.T) {
	if testing.Short() {
		t.Skip("requires Docker")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
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
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := client.Disconnect(context.Background()); err != nil {
			t.Error(err)
		}
	})
	database := client.Database("mobix_http_access_test")
	store := accounts.NewStore(database)
	if err := store.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	accountService, err := accounts.NewService(store)
	if err != nil {
		t.Fatal(err)
	}
	const operatorEmail = "operator@example.test"
	const operatorPassword = "operator-integration-password"
	if err := accountService.Bootstrap(ctx, operatorEmail, operatorPassword); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MOBIX_INTERNAL_KEY", "integration-internal-key-at-least-32-bytes")
	delegation, err := authn.LoadDelegation()
	if err != nil {
		t.Fatal(err)
	}
	ready := func(ctx context.Context) error { return client.Ping(ctx, nil) }
	driverRepository := driverrepo.NewMongoRepository(database)
	if err := driverRepository.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	driverMux := httpapi.Mux("driver", ready)
	driverhandler.Register(driverMux, driverservice.New(driverRepository))
	driver := httptest.NewServer(delegation.Protect("driver", driverMux))
	t.Cleanup(driver.Close)
	tripRepository := triprepo.NewMongoRepository(database)
	// Existing v1 indexes must coexist with independently scoped v2 retry keys.
	_, err = database.Collection("trips").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "idempotency_key", Value: 1}},
		Options: options.Index().SetName("unique_create_key").SetUnique(true).SetSparse(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tripRepository.EnsureIndexes(ctx); err != nil {
		t.Fatal(err)
	}
	tripMux := httpapi.Mux("trip", ready)
	triphandler.Register(tripMux, tripservice.New(tripRepository), tripRepository, driverclient.New(accessURL(t, driver.URL), delegation))
	trip := httptest.NewServer(delegation.Protect("trip", tripMux))
	t.Cleanup(trip.Close)
	payments := paymentrepo.New(client.Database("mobix_http_payment_test"))
	paymentMux := httpapi.Mux("payment", ready)
	paymenthandler.Register(paymentMux, payments, accessURL(t, trip.URL), delegation)
	payment := httptest.NewServer(delegation.Protect("payment", paymentMux))
	t.Cleanup(payment.Close)
	entry := httptest.NewServer(gateway.New(accessURL(t, trip.URL), accessURL(t, driver.URL), accessURL(t, payment.URL),
		accountService, delegation, ready))
	t.Cleanup(entry.Close)
	httpClient := &http.Client{Timeout: 10 * time.Second}
	call := func(token, method, path string, body any, want int, headers http.Header) []byte {
		t.Helper()
		return accessRequest(t, httpClient, entry.URL, token, method, path, body, want, headers)
	}
	login := func(email, password string) accounts.LoginResult {
		t.Helper()
		var session accounts.LoginResult
		data := call("", http.MethodPost, "/v1/auth/login", map[string]string{
			"email": email, "password": password,
		}, http.StatusOK, nil)
		if err := json.Unmarshal(data, &session); err != nil {
			t.Fatal(err)
		}
		if session.Token == "" || session.Account.ID == "" {
			t.Fatal("login response omitted the access token or account ID")
		}
		return session
	}
	operator := login(operatorEmail, operatorPassword)
	fixtureAccount := func(name string, role authn.Role) accounts.LoginResult {
		t.Helper()
		email := name + "@example.test"
		password := "integration-password-" + name
		// Seed the actors for the permission matrix. Account provisioning is
		// exercised separately below and by the full Compose demo.
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		_, err = database.Collection("accounts").InsertOne(ctx, bson.M{
			"_id": bson.NewObjectID().Hex(), "email": email, "role": role, "password_hash": hash,
		})
		if err != nil {
			t.Fatal(err)
		}
		session := login(email, password)
		if session.Account.Role != role {
			t.Fatalf("created account has role %q, want %q", session.Account.Role, role)
		}
		return session
	}
	riderA := fixtureAccount("rider-a", authn.Rider)
	riderB := fixtureAccount("rider-b", authn.Rider)
	driverA := fixtureAccount("driver-a", authn.Driver)
	driverB := fixtureAccount("driver-b", authn.Driver)
	call(operator.Token, http.MethodPost, "/v1/accounts", map[string]string{
		"email": "provisioned@example.test", "password": "provisioned-integration-password", "role": "rider",
	}, http.StatusCreated, nil)
	var provisioned struct {
		PasswordHash []byte `bson:"password_hash"`
	}
	if err := database.Collection("accounts").FindOne(ctx, bson.M{"email": "provisioned@example.test"}).Decode(&provisioned); err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost(provisioned.PasswordHash); err != nil || cost != 12 {
		t.Fatalf("unexpected runtime password hash cost: %d, %v", cost, err)
	}
	for _, token := range []string{"", "invalid-token"} {
		call(token, http.MethodGet, "/v1/auth/me", nil, http.StatusUnauthorized, nil)
	}
	call("", http.MethodPost, "/v1/auth/login", map[string]string{
		"email": riderA.Account.Email, "password": "incorrect-password",
	}, http.StatusUnauthorized, nil)
	call(riderA.Token, http.MethodPost, "/v1/accounts", map[string]string{
		"email": "privilege@example.test", "password": "privilege-password", "role": "operator",
	}, http.StatusForbidden, nil)

	profileBody := map[string]any{
		"name": "Driver A", "email": driverA.Account.Email, "phone": "+12025550101",
		"carNumber": "MBX-101", "carModel": "Toyota Prius", "location": map[string]int{"lat": 0, "lng": 0},
	}
	call(riderA.Token, http.MethodPost, "/v1/drivers", profileBody, http.StatusForbidden, nil)
	profile := accessID(t, call(driverA.Token, http.MethodPost, "/v1/drivers", profileBody, http.StatusCreated, nil))
	call(driverA.Token, http.MethodGet, "/v1/drivers/me", nil, http.StatusOK, nil)
	call(driverB.Token, http.MethodGet, "/v1/drivers/"+profile, nil, http.StatusNotFound, nil)
	call(riderA.Token, http.MethodGet, "/v1/drivers/"+profile, nil, http.StatusForbidden, nil)
	call(driverB.Token, http.MethodPatch, "/v1/drivers/"+profile+"/location", map[string]int{"lat": 1, "lng": 1}, http.StatusNotFound, nil)
	call(operator.Token, http.MethodPatch, "/v1/drivers/"+profile+"/availability", map[string]string{"status": "available"}, http.StatusForbidden, nil)
	call(driverA.Token, http.MethodPatch, "/v1/drivers/"+profile+"/availability", map[string]string{"status": "available"}, http.StatusNoContent, nil)
	call(riderA.Token, http.MethodGet, "/v1/drivers/"+profile+"/activity", nil, http.StatusNotFound, nil)
	for _, upstream := range []string{driver.URL, trip.URL, payment.URL} {
		accessRequest(t, httpClient, upstream, operator.Token, http.MethodGet, "/v1/trips/000000000000000000000001", nil,
			http.StatusUnauthorized, http.Header{authn.IdentityHeader: []string{"forged.operator"}})
	}

	body := map[string]any{
		"pickup":  map[string]float64{"lat": 40.73061, "lng": -73.935242},
		"dropoff": map[string]float64{"lat": 40.74844, "lng": -73.98566},
	}
	const retryKey = "shared-rider-retry-key"
	headers := http.Header{"Idempotency-Key": []string{retryKey}}
	legacyID := bson.NewObjectID()
	digest := sha256.Sum256([]byte(retryKey))
	_, err = database.Collection("trips").InsertOne(ctx, bson.M{
		"_id": legacyID, "rider_id": riderA.Account.ID, "driver_id": profile,
		"status": tripdomain.StatusCompleted, "idempotency_key": hex.EncodeToString(digest[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []accounts.LoginResult{riderA, riderB, driverA, driverB} {
		call(actor.Token, http.MethodGet, "/v1/trips/"+legacyID.Hex(), nil, http.StatusNotFound, nil)
	}
	call(operator.Token, http.MethodGet, "/v1/trips/"+legacyID.Hex(), nil, http.StatusOK, nil)
	tripID := accessID(t, call(riderA.Token, http.MethodPost, "/v1/trips", body, http.StatusCreated, headers))
	if same := accessID(t, call(riderA.Token, http.MethodPost, "/v1/trips", body, http.StatusCreated, headers)); same != tripID {
		t.Fatal("rider retry returned a different trip")
	}
	otherID := accessID(t, call(riderB.Token, http.MethodPost, "/v1/trips", body, http.StatusCreated, headers))
	if otherID == tripID || tripID == legacyID.Hex() {
		t.Fatal("rider scoped retry returned another rider's or a legacy trip")
	}
	changed := map[string]any{"pickup": map[string]int{"lat": 1, "lng": 1}, "dropoff": body["dropoff"]}
	call(riderA.Token, http.MethodPost, "/v1/trips", changed, http.StatusConflict, headers)
	spoofed := map[string]any{"rider_id": riderB.Account.ID, "pickup": body["pickup"], "dropoff": body["dropoff"]}
	call(riderA.Token, http.MethodPost, "/v1/trips", spoofed, http.StatusBadRequest, nil)
	call(driverA.Token, http.MethodPost, "/v1/trips", body, http.StatusForbidden, nil)
	call(operator.Token, http.MethodPost, "/v1/trips", body, http.StatusForbidden, nil)
	call(riderB.Token, http.MethodGet, "/v1/trips/"+tripID, nil, http.StatusNotFound,
		http.Header{authn.IdentityHeader: []string{"forged.operator"}, "X-Mobix-Role": []string{"operator"}})
	statusPath := "/v1/trips/" + tripID + "/status"
	assigned := map[string]string{"status": "assigned", "driver_id": profile}
	call(riderA.Token, http.MethodPatch, statusPath, assigned, http.StatusForbidden, nil)
	call(driverA.Token, http.MethodPatch, statusPath, assigned, http.StatusNotFound, nil)
	call(operator.Token, http.MethodPatch, statusPath, assigned, http.StatusOK, nil)
	stored, err := tripRepository.FindByID(ctx, tripID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RiderAccountID != riderA.Account.ID || stored.DriverAccountID != driverA.Account.ID || stored.DriverID != profile {
		t.Fatal("trip did not persist rider ownership and assigned driver account")
	}
	for _, actor := range []accounts.LoginResult{operator, riderA} {
		call(actor.Token, http.MethodPatch, statusPath, map[string]string{"status": "in_progress"}, http.StatusForbidden, nil)
		call(actor.Token, http.MethodPatch, statusPath, map[string]string{"status": "completed"}, http.StatusForbidden, nil)
	}
	call(driverB.Token, http.MethodGet, "/v1/trips/"+tripID, nil, http.StatusNotFound, nil)
	call(driverB.Token, http.MethodPatch, statusPath, map[string]string{"status": "in_progress"}, http.StatusNotFound, nil)
	call(driverA.Token, http.MethodPatch, statusPath, map[string]string{"status": "completed"}, http.StatusUnprocessableEntity, nil)
	call(driverA.Token, http.MethodPatch, statusPath, map[string]string{"status": "in_progress"}, http.StatusOK, nil)
	call(riderA.Token, http.MethodPatch, statusPath, map[string]string{"status": "cancelled"}, http.StatusUnprocessableEntity, nil)
	call(driverA.Token, http.MethodPatch, statusPath, map[string]string{"status": "completed"}, http.StatusOK, nil)
	call(driverA.Token, http.MethodPatch, statusPath, map[string]string{"status": "completed"}, http.StatusOK, nil)
	for _, actor := range []accounts.LoginResult{riderA, operator} {
		call(actor.Token, http.MethodPatch, statusPath, map[string]string{"status": "completed"}, http.StatusForbidden, nil)
	}
	for _, actor := range []accounts.LoginResult{riderB, driverB} {
		call(actor.Token, http.MethodPatch, statusPath, map[string]string{"status": "completed"}, http.StatusNotFound, nil)
	}
	stored, err = tripRepository.FindByID(ctx, tripID)
	if err != nil || stored.PendingPayment == nil {
		t.Fatalf("completed trip missing persisted payment event: %v", err)
	}
	if err := payments.Record(ctx, *stored.PendingPayment); err != nil {
		t.Fatal(err)
	}
	paymentPath := "/v1/payments/" + tripID
	for _, actor := range []accounts.LoginResult{riderA, operator} {
		call(actor.Token, http.MethodGet, paymentPath, nil, http.StatusOK, nil)
	}
	call(riderB.Token, http.MethodGet, paymentPath, nil, http.StatusNotFound, nil)
	for _, actor := range []accounts.LoginResult{driverA, driverB} {
		call(actor.Token, http.MethodGet, paymentPath, nil, http.StatusForbidden, nil)
	}
	_, err = client.Database("mobix_http_payment_test").Collection("payments").InsertOne(ctx, bson.M{
		"_id": legacyID.Hex(), "status": "simulated_paid", "amount_cents": 500, "currency": "USD",
	})
	if err != nil {
		t.Fatal(err)
	}
	call(operator.Token, http.MethodGet, "/v1/payments/"+legacyID.Hex(), nil, http.StatusOK, nil)
	call(riderA.Token, http.MethodGet, "/v1/payments/"+legacyID.Hex(), nil, http.StatusNotFound, nil)
	call(riderB.Token, http.MethodPatch, "/v1/trips/"+otherID+"/status", map[string]string{"status": "cancelled"}, http.StatusOK, nil)
	call(riderA.Token, http.MethodPatch, "/v1/trips/"+otherID+"/status", map[string]string{"status": "cancelled"}, http.StatusNotFound, nil)
	call(driverB.Token, http.MethodPost, "/v1/auth/logout", nil, http.StatusNoContent, nil)
	call(driverB.Token, http.MethodGet, "/v1/auth/me", nil, http.StatusUnauthorized, nil)
	tokenHash := sha256.Sum256([]byte(riderB.Token))
	_, err = database.Collection("sessions").UpdateOne(ctx, bson.M{"_id": hex.EncodeToString(tokenHash[:])},
		bson.M{"$set": bson.M{"expires_at": time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	call(riderB.Token, http.MethodGet, "/v1/auth/me", nil, http.StatusUnauthorized, nil)
}

func accessURL(t *testing.T, address string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func accessID(t *testing.T, data []byte) string {
	t.Helper()
	var value struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &value); err != nil || value.ID == "" {
		t.Fatalf("response omitted a resource ID: %s", data)
	}
	return value.ID
}

func accessRequest(t *testing.T, client *http.Client, baseURL, token, method, path string, body any, want int, headers http.Header) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	}
	request, err := http.NewRequest(method, baseURL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != want {
		t.Fatalf("%s %s returned HTTP %d, want %d; body %s", method, path, response.StatusCode, want, data)
	}
	return data
}
