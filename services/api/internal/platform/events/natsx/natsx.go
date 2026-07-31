// Package natsx is the JetStream half of ADR-0002. It is the only package that
// imports the NATS SDK: everything else publishes through events.Transport, so
// the relay's logic can be tested without a broker.
package natsx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Config is what the process needs to reach the broker.
type Config struct {
	URL string
	// Name identifies this connection in `nats server report connections`,
	// which is how an operator finds the process holding a stuck subscription.
	Name           string
	ConnectTimeout time.Duration
	// PublishTimeout bounds one publish. It is short on purpose: a relay that
	// blocks on a dead broker stops draining every other event too.
	PublishTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.Name == "" {
		c.Name = "dwellm8"
	}
	if c.ConnectTimeout <= 0 {
		c.ConnectTimeout = 5 * time.Second
	}
	if c.PublishTimeout <= 0 {
		c.PublishTimeout = 10 * time.Second
	}
	return c
}

// Conn is a JetStream connection that satisfies events.Transport.
type Conn struct {
	nc  *nats.Conn
	js  jetstream.JetStream
	cfg Config
}

// Connect dials the broker. It does not fail the caller when the broker is
// down — reconnection is the SDK's job, and ADR-0002 §4 is explicit that an
// unreachable broker accumulates outbox rows rather than failing a request.
func Connect(cfg Config, log *slog.Logger) (*Conn, error) {
	cfg = cfg.withDefaults()

	nc, err := nats.Connect(cfg.URL,
		nats.Name(cfg.Name),
		nats.Timeout(cfg.ConnectTimeout),
		nats.MaxReconnects(-1),
		nats.RetryOnFailedConnect(true),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Warn("nats disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			log.Info("nats reconnected", "url", c.ConnectedUrl())
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("natsx: connecting to %s: %w", cfg.URL, err)
	}

	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, fmt.Errorf("natsx: jetstream: %w", err)
	}
	return &Conn{nc: nc, js: js, cfg: cfg}, nil
}

func (c *Conn) Close() {
	// Drain rather than Close, so in-flight publishes finish before the socket
	// goes; a relay row published-but-unacked is one the sweeper republishes.
	if err := c.nc.Drain(); err != nil {
		c.nc.Close()
	}
}

// JetStream exposes the context for consumers built on this connection.
func (c *Conn) JetStream() jetstream.JetStream { return c.js }

// Publish sends one event and waits for the stream's acknowledgement.
//
// id becomes Nats-Msg-Id, which is what makes a relay retry a duplicate the
// server discards inside the stream's duplicate window rather than a second
// event every consumer has to reason about.
func (c *Conn) Publish(ctx context.Context, subject, id string, body []byte) error {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.PublishTimeout)
	defer cancel()

	_, err := c.js.Publish(ctx, subject, body, jetstream.WithMsgID(id))
	if err != nil {
		return fmt.Errorf("natsx: publishing %s: %w", subject, err)
	}
	return nil
}

// Stream is one module's stream. ADR-0002 §5.
type Stream struct {
	Name     string
	Subjects []string
	MaxAge   time.Duration
}

// Streams is the set the ADR fixes. Money keeps the longest window because it
// is the one anybody will ever want to replay for an audit.
func Streams() []Stream {
	const (
		month = 30 * 24 * time.Hour
		short = 14 * 24 * time.Hour
	)
	return []Stream{
		{Name: "DWELLM8_MONEY", Subjects: []string{"dwellm8.money.>"}, MaxAge: month},
		{Name: "DWELLM8_LEASE", Subjects: []string{"dwellm8.lease.>"}, MaxAge: month},
		{Name: "DWELLM8_MAINTENANCE", Subjects: []string{"dwellm8.maintenance.>"}, MaxAge: short},
		{Name: "DWELLM8_IDENTITY", Subjects: []string{"dwellm8.identity.>"}, MaxAge: short},
		{Name: "DWELLM8_PROPERTY", Subjects: []string{"dwellm8.property.>"}, MaxAge: short},
		{Name: "DWELLM8_COMMUNITY", Subjects: []string{"dwellm8.community.>"}, MaxAge: short},
		{Name: "DWELLM8_DISCOVERY", Subjects: []string{"dwellm8.discovery.>"}, MaxAge: short},
		{Name: "DWELLM8_NOTIFY", Subjects: []string{"dwellm8.notify.>"}, MaxAge: short},
	}
}

// DLQ is where a message lands after max_deliver failures. ADR-0002 §7: it is
// replayed deliberately, after somebody fixes the cause, and never automatically.
const DLQ = "DWELLM8_DLQ"

// EnsureStreams creates or updates every stream, and is safe to run on each
// boot: JetStream treats it as a no-op when the configuration already matches.
//
// The duplicate window is what deduplicates a relay retry, so it is set wider
// than the relay's longest backoff rather than left at the default.
func (c *Conn) EnsureStreams(ctx context.Context) error {
	for _, s := range Streams() {
		_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:       s.Name,
			Subjects:   s.Subjects,
			Retention:  jetstream.LimitsPolicy,
			Storage:    jetstream.FileStorage,
			MaxAge:     s.MaxAge,
			Discard:    jetstream.DiscardOld,
			Duplicates: 30 * time.Minute,
		})
		if err != nil {
			return fmt.Errorf("natsx: ensuring stream %s: %w", s.Name, err)
		}
	}

	_, err := c.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      DLQ,
		Subjects:  []string{"dwellm8.dlq.>"},
		Retention: jetstream.LimitsPolicy,
		Storage:   jetstream.FileStorage,
		MaxAge:    90 * 24 * time.Hour,
		Discard:   jetstream.DiscardOld,
	})
	if err != nil {
		return fmt.Errorf("natsx: ensuring stream %s: %w", DLQ, err)
	}
	return nil
}

// ConsumerSpec names one durable consumer. ADR-0002 §5: durable, explicit-ack,
// pull — pull because a consumer that cannot keep up should build a visible
// backlog rather than be flooded.
type ConsumerSpec struct {
	// Name is <module>-<purpose>, for example notify-payment-receipt.
	Name string
	// Stream it reads.
	Stream string
	// Subjects it filters to.
	Subjects []string
}

// EnsureConsumer creates or updates a durable pull consumer.
func (c *Conn) EnsureConsumer(ctx context.Context, spec ConsumerSpec) (jetstream.Consumer, error) {
	if spec.Name == "" || spec.Stream == "" {
		return nil, errors.New("natsx: a consumer needs a name and a stream")
	}
	cfg := jetstream.ConsumerConfig{
		Durable:       spec.Name,
		AckPolicy:     jetstream.AckExplicitPolicy,
		MaxDeliver:    5,
		AckWait:       30 * time.Second,
		DeliverPolicy: jetstream.DeliverAllPolicy,
	}
	switch len(spec.Subjects) {
	case 0:
	case 1:
		cfg.FilterSubject = spec.Subjects[0]
	default:
		cfg.FilterSubjects = spec.Subjects
	}

	cons, err := c.js.CreateOrUpdateConsumer(ctx, spec.Stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("natsx: ensuring consumer %s on %s: %w", spec.Name, spec.Stream, err)
	}
	return cons, nil
}
