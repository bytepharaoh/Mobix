package realtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
)

func TestTripStreamSnapshotsOrdersAndRedactsEvents(t *testing.T) {
	var snapshotVersion atomic.Int64
	snapshotVersion.Store(2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/trips/trip-a" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"trip-a","version":%d,"status":"requested"}`, snapshotVersion.Load())
	}))
	defer upstream.Close()
	hub, server := streamTestServer(t, upstream.URL, authn.Actor{ID: "rider-a", Role: authn.Rider})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/trips/trip-a/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Last-Event-ID", "ride-event:trip-a:1")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
	reader := bufio.NewReader(response.Body)
	if frame := readFrame(t, reader); frame != "retry: 3000\n" {
		t.Fatalf("retry frame = %q", frame)
	}
	snapshot := readFrame(t, reader)
	if !strings.Contains(snapshot, "id: snapshot:trip-a:2") || !strings.Contains(snapshot, "event: snapshot") ||
		!strings.Contains(snapshot, `"version":2`) {
		t.Fatalf("incorrect snapshot frame: %q", snapshot)
	}

	stale := validEvent("trip-a", 2)
	stale.RiderAccountID = "rider-a"
	hub.Publish(stale)
	live := validEvent("trip-a", 3)
	live.RiderAccountID = "rider-a"
	live.DriverAccountID = "driver-secret"
	live.OfferAccountIDs = []string{"driver-secret", "driver-other"}
	hub.Publish(live)
	frame := readFrame(t, reader)
	if !strings.Contains(frame, "id: ride-event:trip-a:3") || !strings.Contains(frame, "event: trip.status_changed") ||
		!strings.Contains(frame, `"version":3`) {
		t.Fatalf("incorrect live frame: %q", frame)
	}
	if strings.Contains(frame, "rider-a") || strings.Contains(frame, "driver-secret") || strings.Contains(frame, "driver-other") {
		t.Fatalf("audience identifiers leaked to client: %q", frame)
	}

	snapshotVersion.Store(4)
	hub.Resync()
	resync := readFrame(t, reader)
	if !strings.Contains(resync, "id: snapshot:trip-a:4") || !strings.Contains(resync, `"version":4`) {
		t.Fatalf("broker resync did not refresh snapshot: %q", resync)
	}
}

func TestOfferStreamAuthorizesDriverAndFiltersAudience(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/drivers/me/offers" {
			http.NotFound(w, r)
			return
		}
		io.WriteString(w, `[{"id":"offer-a","trip_id":"trip-a","version":1}]`)
	}))
	defer upstream.Close()
	hub, server := streamTestServer(t, upstream.URL, authn.Actor{ID: "driver-a", Role: authn.Driver})
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v1/drivers/me/offers/events", nil)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	readFrame(t, reader)
	snapshot := readFrame(t, reader)
	if !strings.Contains(snapshot, `"trip_id":"trip-a"`) {
		t.Fatalf("incorrect offers snapshot: %q", snapshot)
	}

	notForDriver := validEvent("trip-b", 1)
	notForDriver.OfferAccountIDs = []string{"driver-b"}
	hub.Publish(notForDriver)
	forDriver := validEvent("trip-a", 2)
	forDriver.Type = messaging.RideOffersUpdated
	forDriver.ID = "ride-event:trip-a:2"
	forDriver.OfferAccountIDs = []string{"driver-a", "driver-b"}
	hub.Publish(forDriver)
	frame := readFrame(t, reader)
	if !strings.Contains(frame, "event: offers.updated") || strings.Contains(frame, "driver-b") {
		t.Fatalf("incorrect or unredacted driver frame: %q", frame)
	}
}

func TestOfferStreamRejectsNonDriversBeforeOpeningStream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, `[]`)
	}))
	defer upstream.Close()
	_, server := streamTestServer(t, upstream.URL, authn.Actor{ID: "rider-a", Role: authn.Rider})
	defer server.Close()
	response, err := http.Get(server.URL + "/v1/drivers/me/offers/events")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden || !strings.Contains(response.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("non-driver response = %d %q", response.StatusCode, response.Header.Get("Content-Type"))
	}
}

func streamTestServer(t *testing.T, upstream string, actor authn.Actor) (*Hub, *httptest.Server) {
	t.Helper()
	tripURL, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	config := DefaultConfig()
	config.HeartbeatInterval = time.Minute
	config.MaxLifetime = time.Hour
	hub := NewHub(config)
	stream := NewHTTP(hub, tripURL, authn.Delegation{}, config)
	mux := http.NewServeMux()
	stream.Register(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(authn.WithActor(r.Context(), actor)))
		})
	}, nil)
	return hub, httptest.NewServer(mux)
}

func readFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	result := make(chan struct {
		value string
		err   error
	}, 1)
	go func() {
		var frame strings.Builder
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				result <- struct {
					value string
					err   error
				}{frame.String(), err}
				return
			}
			if line == "\n" {
				result <- struct {
					value string
					err   error
				}{frame.String(), nil}
				return
			}
			frame.WriteString(line)
		}
	}()
	select {
	case value := <-result:
		if value.err != nil {
			t.Fatalf("read SSE frame: %v", value.err)
		}
		return value.value
	case <-time.After(2 * time.Second):
		t.Fatal("timed out reading SSE frame")
		return ""
	}
}
