package flow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCapturingLogger returns a *slog.Logger whose output is captured into buf
// as JSON, one record per line — convenient for asserting on attributes.
func newCapturingLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

// records parses one JSON object per line out of buf.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
		out = append(out, rec)
	}
	return out
}

func TestLoggerKey(t *testing.T) {
	t.Run("FromOr falls back to default when ctx has no logger", func(t *testing.T) {
		def := slog.Default()
		got := Logger.FromOr(context.Background(), def)
		assert.Same(t, def, got)
	})

	t.Run("With + From round-trips a *slog.Logger", func(t *testing.T) {
		var buf bytes.Buffer
		want := newCapturingLogger(&buf)
		ctx := Logger.With(context.Background(), want)
		got, ok := Logger.From(ctx)
		assert.True(t, ok)
		assert.Same(t, want, got)
	})
}

func TestLogStepFields(t *testing.T) {
	t.Run("binds step=<name> onto the ctx logger", func(t *testing.T) {
		var buf bytes.Buffer
		base := newCapturingLogger(&buf)
		ctx := Logger.With(context.Background(), base)

		step := &NamedStep{Name: "MyStep", Steper: NoOp("inner")}

		ic := LogStepFields()
		err := ic.InterceptStep(ctx, step, func(ctx context.Context) error {
			Logger.FromOr(ctx, slog.Default()).Info("hello", "k", "v")
			return nil
		})
		require.NoError(t, err)

		recs := records(t, &buf)
		require.Len(t, recs, 1)
		assert.Equal(t, "hello", recs[0]["msg"])
		assert.Equal(t, "MyStep", recs[0]["step"])
		assert.Equal(t, "v", recs[0]["k"])
	})

	t.Run("extra functions append additional fields", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := Logger.With(context.Background(), newCapturingLogger(&buf))

		step := &NamedStep{Name: "S", Steper: NoOp("inner")}

		ic := LogStepFields(
			func(ctx context.Context, s Steper) []any { return []any{"tenant", "acme"} },
			func(ctx context.Context, s Steper) []any { return []any{"region", "westus2"} },
		)
		err := ic.InterceptStep(ctx, step, func(ctx context.Context) error {
			Logger.FromOr(ctx, slog.Default()).Info("hi")
			return nil
		})
		require.NoError(t, err)

		recs := records(t, &buf)
		require.Len(t, recs, 1)
		assert.Equal(t, "S", recs[0]["step"])
		assert.Equal(t, "acme", recs[0]["tenant"])
		assert.Equal(t, "westus2", recs[0]["region"])
	})

	t.Run("uses slog.Default() when ctx has no logger", func(t *testing.T) {
		// Replace the default temporarily so we can capture its output.
		var buf bytes.Buffer
		old := slog.Default()
		slog.SetDefault(newCapturingLogger(&buf))
		t.Cleanup(func() { slog.SetDefault(old) })

		step := &NamedStep{Name: "Default", Steper: NoOp("inner")}
		ic := LogStepFields()
		err := ic.InterceptStep(context.Background(), step, func(ctx context.Context) error {
			Logger.FromOr(ctx, slog.Default()).Info("from default")
			return nil
		})
		require.NoError(t, err)

		recs := records(t, &buf)
		require.Len(t, recs, 1)
		assert.Equal(t, "Default", recs[0]["step"])
	})

	t.Run("does not pollute the original ctx logger", func(t *testing.T) {
		var buf bytes.Buffer
		base := newCapturingLogger(&buf)
		ctx := Logger.With(context.Background(), base)
		step := &NamedStep{Name: "S", Steper: NoOp("inner")}

		ic := LogStepFields()
		err := ic.InterceptStep(ctx, step, func(ctx context.Context) error { return nil })
		require.NoError(t, err)

		// The base logger should not have step=... attached. Logging through
		// it directly produces a record without "step".
		base.Info("plain")
		recs := records(t, &buf)
		require.Len(t, recs, 1)
		_, has := recs[0]["step"]
		assert.False(t, has, "base logger must not be mutated")
	})

	t.Run("propagates next error", func(t *testing.T) {
		ctx := Logger.With(context.Background(), slog.Default())
		step := &NamedStep{Name: "S", Steper: NoOp("inner")}
		want := errors.New("boom")

		ic := LogStepFields()
		got := ic.InterceptStep(ctx, step, func(ctx context.Context) error { return want })
		assert.ErrorIs(t, got, want)
	})
}

func TestLogAttemptField(t *testing.T) {
	t.Run("binds attempt=N onto the ctx logger", func(t *testing.T) {
		var buf bytes.Buffer
		ctx := Logger.With(context.Background(), newCapturingLogger(&buf))
		step := &NamedStep{Name: "S", Steper: NoOp("inner")}

		ic := LogAttemptField()
		err := ic.InterceptAttempt(ctx, step, 2, func(ctx context.Context) error {
			Logger.FromOr(ctx, slog.Default()).Info("try")
			return nil
		})
		require.NoError(t, err)

		recs := records(t, &buf)
		require.Len(t, recs, 1)
		assert.EqualValues(t, 2, recs[0]["attempt"])
	})
}
