package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/bytepharaoh/Mobix/pkg/authn"
	apperrors "github.com/bytepharaoh/Mobix/pkg/errors"
	"github.com/bytepharaoh/Mobix/pkg/httpapi"
	"github.com/bytepharaoh/Mobix/pkg/messaging"
)

const (
	TripStreamRoute   = "GET /v1/trips/{id}/events"
	OffersStreamRoute = "GET /v1/drivers/me/offers/events"
)

var errStreamCapacity = apperrors.New(http.StatusServiceUnavailable, "STREAM_CAPACITY", "Real-time stream capacity is temporarily exhausted")

type HTTP struct {
	hub        *Hub
	trip       *url.URL
	delegation authn.Delegation
	config     Config
	client     *http.Client
}

func NewHTTP(hub *Hub, trip *url.URL, delegation authn.Delegation, config Config) *HTTP {
	return &HTTP{
		hub: hub, trip: trip, delegation: delegation, config: config,
		client: &http.Client{
			Timeout:       5 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

func (stream *HTTP) Register(mux *http.ServeMux, require func(http.Handler) http.Handler, limit func(http.Handler) http.Handler) {
	if stream == nil || stream.hub == nil {
		return
	}
	wrap := func(handler http.Handler) http.Handler {
		if limit != nil {
			handler = limit(handler)
		}
		return require(handler)
	}
	mux.Handle(TripStreamRoute, wrap(http.HandlerFunc(stream.tripEvents)))
	mux.Handle(OffersStreamRoute, wrap(http.HandlerFunc(stream.offerEvents)))
}

func (stream *HTTP) tripEvents(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Get("Last-Event-ID")) > 128 {
		httpapi.Error(w, apperrors.ErrInvalidInput)
		return
	}
	actor := authn.From(r.Context())
	tripID := r.PathValue("id")
	subscription, err := stream.hub.Subscribe(accountKey(actor), func(event messaging.RideEvent) bool {
		return event.TripID == tripID
	})
	if err != nil {
		stream.capacityError(w)
		return
	}
	defer subscription.Close()
	snapshot := func(ctx context.Context) (streamSnapshot, error) {
		body, err := stream.fetch(ctx, actor, "/v1/trips/"+url.PathEscape(tripID))
		if err != nil {
			return streamSnapshot{}, err
		}
		var trip struct {
			ID      string `json:"id"`
			Version int64  `json:"version"`
		}
		if json.Unmarshal(body, &trip) != nil || trip.ID != tripID || trip.Version < 0 {
			return streamSnapshot{}, apperrors.ErrUnavailable
		}
		return streamSnapshot{
			body: body, id: fmt.Sprintf("snapshot:%s:%d", trip.ID, trip.Version),
			versions: map[string]int64{trip.ID: trip.Version},
		}, nil
	}
	initial, err := snapshot(r.Context())
	if err != nil {
		writeSnapshotError(w, err)
		return
	}
	versions := lastEventVersions(r.Header.Get("Last-Event-ID"))
	stream.serve(w, r, subscription, initial, snapshot, versions)
}

func (stream *HTTP) offerEvents(w http.ResponseWriter, r *http.Request) {
	if len(r.Header.Get("Last-Event-ID")) > 128 {
		httpapi.Error(w, apperrors.ErrInvalidInput)
		return
	}
	actor := authn.From(r.Context())
	if actor.Role != authn.Driver || actor.ID == "" {
		httpapi.Error(w, apperrors.ErrForbidden)
		return
	}
	subscription, err := stream.hub.Subscribe(accountKey(actor), func(event messaging.RideEvent) bool {
		return event.DriverAccountID == actor.ID || slices.Contains(event.OfferAccountIDs, actor.ID)
	})
	if err != nil {
		stream.capacityError(w)
		return
	}
	defer subscription.Close()
	snapshot := func(ctx context.Context) (streamSnapshot, error) {
		body, err := stream.fetch(ctx, actor, "/v1/drivers/me/offers")
		if err != nil {
			return streamSnapshot{}, err
		}
		var offers []struct {
			TripID  string `json:"trip_id"`
			Version int64  `json:"version"`
		}
		if json.Unmarshal(body, &offers) != nil {
			return streamSnapshot{}, apperrors.ErrUnavailable
		}
		versions := make(map[string]int64, len(offers))
		for _, offer := range offers {
			if offer.TripID == "" || offer.Version < 1 {
				return streamSnapshot{}, apperrors.ErrUnavailable
			}
			versions[offer.TripID] = max(versions[offer.TripID], offer.Version)
		}
		return streamSnapshot{body: body, versions: versions}, nil
	}
	initial, err := snapshot(r.Context())
	if err != nil {
		writeSnapshotError(w, err)
		return
	}
	versions := lastEventVersions(r.Header.Get("Last-Event-ID"))
	stream.serve(w, r, subscription, initial, snapshot, versions)
}

type streamSnapshot struct {
	body     json.RawMessage
	id       string
	versions map[string]int64
}

type publicEvent struct {
	ID         string                  `json:"id"`
	TripID     string                  `json:"trip_id"`
	Type       messaging.RideEventType `json:"type"`
	Status     string                  `json:"status"`
	Version    int64                   `json:"version"`
	OccurredAt time.Time               `json:"occurred_at"`
}

func (stream *HTTP) serve(w http.ResponseWriter, r *http.Request, subscription *Subscription, initial streamSnapshot,
	snapshot func(context.Context) (streamSnapshot, error), versions map[string]int64,
) {
	if disconnected(subscription) {
		stream.capacityError(w)
		return
	}
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
		httpapi.Error(w, apperrors.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if _, err := io.WriteString(w, "retry: 3000\n\n"); err != nil {
		return
	}
	mergeVersions(versions, initial.versions)
	if err := stream.writeEvent(w, "snapshot", initial.id, initial.body); err != nil {
		return
	}
	heartbeat := time.NewTicker(stream.config.HeartbeatInterval)
	defer heartbeat.Stop()
	lifetime := time.NewTimer(stream.config.MaxLifetime)
	defer lifetime.Stop()
	for {
		if disconnected(subscription) {
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-subscription.Done:
			return
		case <-lifetime.C:
			return
		case <-heartbeat.C:
			if err := stream.writeHeartbeat(w); err != nil {
				return
			}
		case notification := <-subscription.Events:
			if notification.Resync {
				if err := stream.refresh(w, r.Context(), snapshot, versions); err != nil {
					return
				}
				continue
			}
			event := notification.Event
			if event.Version <= versions[event.TripID] {
				continue
			}
			knownVersion, known := versions[event.TripID]
			if event.Version > knownVersion+1 && (known || event.Version > 1) {
				if err := stream.refresh(w, r.Context(), snapshot, versions); err != nil {
					return
				}
				if event.Version <= versions[event.TripID] {
					continue
				}
			}
			value := publicEvent{
				ID: event.ID, TripID: event.TripID, Type: event.Type, Status: event.Status,
				Version: event.Version, OccurredAt: event.OccurredAt,
			}
			if err := stream.writeEvent(w, string(event.Type), event.ID, value); err != nil {
				return
			}
			versions[event.TripID] = event.Version
		}
	}
}

func (stream *HTTP) refresh(w http.ResponseWriter, ctx context.Context,
	snapshot func(context.Context) (streamSnapshot, error), versions map[string]int64,
) error {
	current, err := snapshot(ctx)
	if err != nil {
		return err
	}
	mergeVersions(versions, current.versions)
	return stream.writeEvent(w, "snapshot", current.id, current.body)
}

func (stream *HTTP) writeEvent(w http.ResponseWriter, event, id string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var frame bytes.Buffer
	if id != "" {
		fmt.Fprintf(&frame, "id: %s\n", id)
	}
	if event != "" {
		fmt.Fprintf(&frame, "event: %s\n", event)
	}
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		frame.WriteString("data: ")
		frame.Write(line)
		frame.WriteByte('\n')
	}
	frame.WriteByte('\n')
	return stream.write(w, frame.Bytes())
}

func (stream *HTTP) writeHeartbeat(w http.ResponseWriter) error {
	return stream.write(w, []byte(": heartbeat\n\n"))
}

func (stream *HTTP) write(w http.ResponseWriter, data []byte) error {
	controller := http.NewResponseController(w)
	if err := controller.SetWriteDeadline(time.Now().Add(stream.config.WriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
		return nil
	}
	return http.ErrNotSupported
}

func (stream *HTTP) fetch(ctx context.Context, actor authn.Actor, path string) (json.RawMessage, error) {
	target := stream.trip.JoinPath(strings.TrimPrefix(path, "/"))
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, err
	}
	stream.delegation.Sign(request, "trip", actor)
	response, err := stream.client.Do(request)
	if err != nil {
		return nil, apperrors.ErrUnavailable
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, httpapi.MaxBody+1))
	if err != nil || len(body) > httpapi.MaxBody {
		return nil, apperrors.ErrUnavailable
	}
	if response.StatusCode != http.StatusOK {
		return nil, snapshotError{status: response.StatusCode, body: body}
	}
	if !json.Valid(body) {
		return nil, apperrors.ErrUnavailable
	}
	return body, nil
}

type snapshotError struct {
	status int
	body   []byte
}

func (err snapshotError) Error() string {
	return fmt.Sprintf("snapshot upstream returned %d", err.status)
}

func writeSnapshotError(w http.ResponseWriter, err error) {
	var upstream snapshotError
	if errors.As(err, &upstream) && upstream.status >= 400 && upstream.status <= 599 && json.Valid(upstream.body) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(upstream.status)
		_, _ = w.Write(upstream.body)
		return
	}
	httpapi.Error(w, err)
}

func (stream *HTTP) capacityError(w http.ResponseWriter) {
	w.Header().Set("Retry-After", "3")
	httpapi.Error(w, errStreamCapacity)
}

func accountKey(actor authn.Actor) string { return string(actor.Role) + ":" + actor.ID }

func disconnected(subscription *Subscription) bool {
	select {
	case <-subscription.Done:
		return true
	default:
		return false
	}
}

func mergeVersions(target, values map[string]int64) {
	for tripID, version := range values {
		target[tripID] = max(target[tripID], version)
	}
}

func lastEventVersions(id string) map[string]int64 {
	versions := make(map[string]int64, 1)
	parts := strings.Split(id, ":")
	if len(parts) != 3 || parts[0] != "ride-event" && parts[0] != "snapshot" || parts[1] == "" {
		return versions
	}
	version, err := strconv.ParseInt(parts[2], 10, 64)
	if err == nil && version >= 0 {
		versions[parts[1]] = version
	}
	return versions
}
