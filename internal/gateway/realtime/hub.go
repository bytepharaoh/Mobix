package realtime

import (
	"errors"
	"sync"

	"github.com/bytepharaoh/Mobix/pkg/messaging"
)

const seenEventLimit = 10_000

var ErrConnectionLimit = errors.New("real-time connection limit reached")

type Notification struct {
	Event  messaging.RideEvent
	Resync bool
}

type Subscription struct {
	hub      *Hub
	id       uint64
	Events   <-chan Notification
	Done     <-chan struct{}
	events   chan Notification
	done     chan struct{}
	account  string
	matches  func(messaging.RideEvent) bool
	closeOne sync.Once
}

func (subscription *Subscription) Close() {
	if subscription == nil || subscription.hub == nil {
		return
	}
	subscription.closeOne.Do(func() { subscription.hub.remove(subscription.id) })
}

type Hub struct {
	mu            sync.Mutex
	config        Config
	nextID        uint64
	subscriptions map[uint64]*Subscription
	accounts      map[string]int
	seen          map[string]struct{}
	seenOrder     []string
	seenCursor    int
}

func NewHub(config Config) *Hub {
	return &Hub{
		config: config, subscriptions: make(map[uint64]*Subscription), accounts: make(map[string]int),
		seen: make(map[string]struct{}, seenEventLimit), seenOrder: make([]string, 0, seenEventLimit),
	}
}

func (hub *Hub) Subscribe(account string, matches func(messaging.RideEvent) bool) (*Subscription, error) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if account == "" || len(hub.subscriptions) >= hub.config.MaxConnections ||
		hub.accounts[account] >= hub.config.MaxConnectionsPerAccount {
		return nil, ErrConnectionLimit
	}
	hub.nextID++
	events := make(chan Notification, hub.config.SubscriberBuffer)
	done := make(chan struct{})
	subscription := &Subscription{
		hub: hub, id: hub.nextID, Events: events, Done: done, events: events, done: done,
		account: account, matches: matches,
	}
	hub.subscriptions[subscription.id] = subscription
	hub.accounts[account]++
	return subscription, nil
}

func (hub *Hub) Publish(event messaging.RideEvent) {
	if !event.Valid() {
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if _, duplicate := hub.seen[event.ID]; duplicate {
		return
	}
	hub.remember(event.ID)
	for id, subscription := range hub.subscriptions {
		if subscription.matches != nil && !subscription.matches(event) {
			continue
		}
		if !hub.send(subscription, Notification{Event: event}) {
			hub.removeLocked(id)
		}
	}
}

func (hub *Hub) Resync() {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	for id, subscription := range hub.subscriptions {
		if !hub.send(subscription, Notification{Resync: true}) {
			hub.removeLocked(id)
		}
	}
}

func (hub *Hub) send(subscription *Subscription, notification Notification) bool {
	select {
	case subscription.events <- notification:
		return true
	default:
		return false
	}
}

func (hub *Hub) remember(id string) {
	if len(hub.seenOrder) < seenEventLimit {
		hub.seenOrder = append(hub.seenOrder, id)
	} else {
		delete(hub.seen, hub.seenOrder[hub.seenCursor])
		hub.seenOrder[hub.seenCursor] = id
		hub.seenCursor = (hub.seenCursor + 1) % seenEventLimit
	}
	hub.seen[id] = struct{}{}
}

func (hub *Hub) remove(id uint64) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	hub.removeLocked(id)
}

func (hub *Hub) removeLocked(id uint64) {
	subscription, exists := hub.subscriptions[id]
	if !exists {
		return
	}
	delete(hub.subscriptions, id)
	hub.accounts[subscription.account]--
	if hub.accounts[subscription.account] == 0 {
		delete(hub.accounts, subscription.account)
	}
	close(subscription.done)
}
