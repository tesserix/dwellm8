package temporalx

import (
	"log/slog"

	sdklog "go.temporal.io/sdk/log"
)

// bridge sends the SDK's logging into the process's slog handler, so a worker's
// output is the same shape as everything else in the namespace and one query
// finds a run across the API and the worker alike.
type bridge struct{ log *slog.Logger }

func newLogger(l *slog.Logger) sdklog.Logger { return bridge{log: l} }

func (b bridge) Debug(msg string, kv ...any) { b.log.Debug(msg, kv...) }
func (b bridge) Info(msg string, kv ...any)  { b.log.Info(msg, kv...) }
func (b bridge) Warn(msg string, kv ...any)  { b.log.Warn(msg, kv...) }
func (b bridge) Error(msg string, kv ...any) { b.log.Error(msg, kv...) }

// With satisfies the SDK's optional structured logger, which is what makes the
// workflow id and run id appear on every line the SDK emits.
func (b bridge) With(kv ...any) sdklog.Logger { return bridge{log: b.log.With(kv...)} }
