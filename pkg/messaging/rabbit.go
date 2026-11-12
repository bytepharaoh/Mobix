package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

const deadLetterQueue = CompletedQueue + ".dead"

var ErrInvalidEvent = errors.New("invalid trip completion event")

type Outbox interface {
	NextPayment(context.Context) (*TripCompleted, error)
	MarkPaymentPublished(context.Context, TripCompleted) error
}

type RideOutbox interface {
	NextRideEvent(context.Context) (*RideEvent, error)
	MarkRideEventPublished(context.Context, RideEvent) error
}

func RunPublisher(ctx context.Context, uri string, outbox Outbox) {
	run(ctx, uri, "publisher", func(ch *amqp.Channel) error {
		if err := declarePaymentTopology(ch); err != nil {
			return err
		}
		if err := ch.Confirm(false); err != nil {
			return err
		}
		returns := ch.NotifyReturn(make(chan amqp.Return, 1))
		closed := ch.NotifyClose(make(chan *amqp.Error, 1))
		for ctx.Err() == nil {
			event, err := outbox.NextPayment(ctx)
			if err != nil {
				return fmt.Errorf("read outbox: %w", err)
			}
			if event == nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-closed:
					return errors.New("publisher channel closed")
				case <-time.After(300 * time.Millisecond):
					continue
				}
			}
			if err := publishPayment(ctx, ch, returns, *event); err != nil {
				return err
			}
			if err := outbox.MarkPaymentPublished(ctx, *event); err != nil {
				return fmt.Errorf("mark outbox event: %w", err)
			}
		}
		return ctx.Err()
	})
}

func RunRidePublisher(ctx context.Context, uri string, outbox RideOutbox) {
	run(ctx, uri, "ride-event-publisher", func(ch *amqp.Channel) error {
		if err := declareRideTopology(ch); err != nil {
			return err
		}
		if err := ch.Confirm(false); err != nil {
			return err
		}
		returns := ch.NotifyReturn(make(chan amqp.Return, 1))
		closed := ch.NotifyClose(make(chan *amqp.Error, 1))
		for ctx.Err() == nil {
			event, err := outbox.NextRideEvent(ctx)
			if err != nil {
				return fmt.Errorf("read ride event outbox: %w", err)
			}
			if event == nil {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-closed:
					return errors.New("ride event publisher channel closed")
				case <-time.After(300 * time.Millisecond):
					continue
				}
			}
			if err := publishRideEvent(ctx, ch, returns, *event); err != nil {
				return err
			}
			if err := outbox.MarkRideEventPublished(ctx, *event); err != nil {
				return fmt.Errorf("mark ride event published: %w", err)
			}
		}
		return ctx.Err()
	})
}

func publishPayment(ctx context.Context, ch *amqp.Channel, returns <-chan amqp.Return, event TripCompleted) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return publishConfirmed(ctx, ch, returns, "", CompletedQueue, CompletedKind, event.ID, event.OccurredAt, body)
}

func publishRideEvent(ctx context.Context, ch *amqp.Channel, returns <-chan amqp.Return, event RideEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return publishConfirmed(ctx, ch, returns, RideExchange, string(event.Type), RideEventKind, event.ID, event.OccurredAt, body)
}

func publishConfirmed(ctx context.Context, ch *amqp.Channel, returns <-chan amqp.Return,
	exchange, routingKey, eventType, messageID string, occurredAt time.Time, body []byte,
) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	confirmation, err := ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, true, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Type:         eventType,
		MessageId:    messageID,
		Timestamp:    occurredAt,
		Body:         body,
	})
	if err != nil {
		return fmt.Errorf("publish event: %w", err)
	}
	confirmed, err := confirmation.WaitContext(ctx)
	if err != nil {
		return fmt.Errorf("wait for publish confirmation: %w", err)
	}
	if !confirmed {
		return errors.New("broker declined the event")
	}
	// RabbitMQ sends an unroutable mandatory return before its confirmation.
	select {
	case <-returns:
		return errors.New("broker could not route the event")
	default:
		return nil
	}
}

func RunConsumer(ctx context.Context, uri string, handle func(context.Context, TripCompleted) error) {
	run(ctx, uri, "consumer", func(ch *amqp.Channel) error {
		if err := declarePaymentTopology(ch); err != nil {
			return err
		}
		if err := ch.Qos(1, 0, false); err != nil {
			return err
		}
		deliveries, err := ch.ConsumeWithContext(ctx, CompletedQueue, "", false, false, false, false, nil)
		if err != nil {
			return err
		}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case delivery, ok := <-deliveries:
				if !ok {
					return errors.New("consumer deliveries closed")
				}
				if err := process(ctx, delivery, handle); err != nil {
					return err
				}
			}
		}
	})
}

func process(ctx context.Context, delivery amqp.Delivery, handle func(context.Context, TripCompleted) error) error {
	var event TripCompleted
	if delivery.Type != CompletedKind || len(delivery.Body) > 8<<10 ||
		json.Unmarshal(delivery.Body, &event) != nil || delivery.MessageId != event.ID {
		slog.Warn("dead-lettering malformed payment event")
		return delivery.Reject(false)
	}
	err := handle(ctx, event)
	if errors.Is(err, ErrInvalidEvent) {
		slog.Warn("dead-lettering invalid payment event")
		return delivery.Reject(false)
	}
	if err != nil {
		// Closing the channel requeues this unacknowledged delivery.
		return fmt.Errorf("process payment event: %w", err)
	}
	return delivery.Ack(false)
}

func run(ctx context.Context, uri, worker string, work func(*amqp.Channel) error) {
	for ctx.Err() == nil {
		err := connect(ctx, uri, work)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("RabbitMQ worker will reconnect", "worker", worker, "error", err)
		select {
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
	}
}

type timedWriter struct{ net.Conn }

func (conn timedWriter) Write(data []byte) (int, error) {
	if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return 0, err
	}
	return conn.Conn.Write(data)
}

func connect(ctx context.Context, uri string, work func(*amqp.Channel) error) error {
	var stop func() bool
	connection, err := amqp.DialConfig(uri, amqp.Config{
		Heartbeat: 10 * time.Second,
		Dial: func(network, address string) (net.Conn, error) {
			conn, err := amqp.DefaultDial(4*time.Second)(network, address)
			if err != nil {
				return nil, err
			}
			stop = context.AfterFunc(ctx, func() { _ = conn.Close() })
			return timedWriter{conn}, nil
		},
	})
	if stop != nil {
		defer stop()
	}
	if err != nil {
		return fmt.Errorf("connect to RabbitMQ: %w", err)
	}
	defer connection.CloseDeadline(time.Now().Add(2 * time.Second))
	channel, err := connection.Channel()
	if err != nil {
		return err
	}
	defer channel.Close()
	return work(channel)
}

func declarePaymentTopology(channel *amqp.Channel) error {
	if _, err := channel.QueueDeclare(deadLetterQueue, true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := channel.QueueDeclare(CompletedQueue, true, false, false, false, amqp.Table{
		"x-dead-letter-exchange": "", "x-dead-letter-routing-key": deadLetterQueue,
	}); err != nil {
		return err
	}
	return nil
}

func declareRideTopology(channel *amqp.Channel) error {
	if err := channel.ExchangeDeclare(RideExchange, "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := channel.QueueDeclare(RideBufferQueue, true, false, false, false, amqp.Table{
		"x-message-ttl": int32((10 * time.Minute) / time.Millisecond),
		"x-max-length":  int32(10_000),
		"x-overflow":    "drop-head",
	}); err != nil {
		return err
	}
	return channel.QueueBind(RideBufferQueue, "#", RideExchange, false, nil)
}
