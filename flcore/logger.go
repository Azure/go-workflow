package flcore

import (
	"context"
	"log/slog"
	"testing"

	"github.com/neilotoole/slogt"
)

type Logger interface {
	With(args ...any) Logger
	WithGroup(name string) Logger
	Debug(msg string, args ...any)
	DebugContext(ctx context.Context, msg string, args ...any)
	Info(msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	Warn(msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	Error(msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

type testLogger struct {
	*slog.Logger
}

func NewTestLogger(t *testing.T, opt ...slogt.Option) Logger {
	return &testLogger{slogt.New(t, opt...)}
}
func (l *testLogger) With(args ...any) Logger { return &testLogger{l.Logger.With(args...)} }
func (l *testLogger) WithGroup(name string) Logger {
	return &testLogger{l.Logger.WithGroup(name)}
}
