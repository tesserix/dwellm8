package natsx

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Consume pulls a durable consumer until the context ends, handing each
// message to handle. Nil acks; an error naks, so JetStream redelivers up to
// MaxDeliver and then parks the message — at-least-once end to end, which is
// why every handler fed to this must be idempotent (ADR-0002 §5).
func Consume(ctx context.Context, cons jetstream.Consumer, handle func(context.Context, []byte) error, log *slog.Logger) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		batch, err := cons.Fetch(25, jetstream.FetchMaxWait(5*time.Second))
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				continue
			}
			log.Warn("consumer fetch failed", "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for msg := range batch.Messages() {
			if err := handle(ctx, msg.Data()); err != nil {
				log.Warn("event handling failed; redelivery will retry", "error", err)
				_ = msg.Nak()
				continue
			}
			_ = msg.Ack()
		}
	}
}
