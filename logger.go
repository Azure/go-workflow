package flow

import (
	"context"
	"log/slog"
)

// Logger is the canonical ContextKey for a *slog.Logger flowing through a
// Workflow. Inject one with Logger.With and read it back inside a Steper
// with Logger.FromOr(ctx, slog.Default()) so steps that did not bother to
// configure a logger still work:
//
//	ctx = flow.Logger.With(ctx, mySlog)
//
//	func (s *MyStep) Do(ctx context.Context) error {
//	    log := flow.Logger.FromOr(ctx, slog.Default())
//	    log.Info("doing work", "name", s.Name)
//	    return nil
//	}
//
// Compose with LogStepFields and LogAttemptField to automatically tag
// every log line with step=<name> and attempt=N.
var Logger = ContextKey[*slog.Logger]{}

// LogStepFields returns a StepInterceptor that derives the ctx logger by
// binding step=<flow.String(step)> (and any extra fields the caller
// supplies) onto it, so Steper implementations can write:
//
//	log := flow.Logger.FromOr(ctx, slog.Default())
//	log.Info("creating", "name", s.Name)
//
// and get step=<name> for free without each Steper having to bind it
// manually. Extra functions append slog-style key/value pairs:
//
//	flow.LogStepFields(func(ctx context.Context, _ flow.Steper) []any {
//	    return []any{"tenant", tenantFrom(ctx)}
//	})
//
// If ctx has no logger, slog.Default() is used as the base. The original
// logger in ctx is not mutated — every step run sees a freshly derived
// logger so step-level fields do not accumulate across steps.
func LogStepFields(extra ...func(context.Context, Steper) []any) StepInterceptor {
	return StepInterceptorFunc(func(ctx context.Context, step Steper, next func(context.Context) error) error {
		base := Logger.FromOr(ctx, slog.Default())
		attrs := []any{"step", String(step)}
		for _, fn := range extra {
			attrs = append(attrs, fn(ctx, step)...)
		}
		ctx = Logger.With(ctx, base.With(attrs...))
		return next(ctx)
	})
}

// LogAttemptField is the AttemptInterceptor counterpart of LogStepFields:
// it binds attempt=<n> onto the ctx logger inside the retry loop. Compose
// with LogStepFields to also tag the step name on every attempt.
func LogAttemptField() AttemptInterceptor {
	return AttemptInterceptorFunc(func(ctx context.Context, step Steper, attempt uint64, next func(context.Context) error) error {
		base := Logger.FromOr(ctx, slog.Default())
		ctx = Logger.With(ctx, base.With("attempt", attempt))
		return next(ctx)
	})
}
