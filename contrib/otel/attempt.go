package flowotel

import (
	"context"
	"fmt"

	flow "github.com/Azure/go-workflow"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// NewAttemptInterceptor returns a flow.AttemptInterceptor that emits one
// OpenTelemetry span per attempt (one per call into the retry loop).
//
// The span name defaults to fmt.Sprintf("%s (attempt %d)", flow.String(step),
// attempt) and may be overridden via WithAttemptSpanNamer. The default
// attribute set always includes workflow.step.name = flow.String(step) and
// workflow.step.attempt = int64(attempt). Extra attributes can be supplied
// via WithAttemptAttributes; they are appended to (not in place of) the
// defaults at span-start time. Canonical attributes (workflow.step.name,
// workflow.step.attempt) always win over user-supplied attributes — i.e.,
// WithAttemptAttributes cannot override them.
//
// On a non-nil error from next() the span records the error via
// span.RecordError and sets its status to codes.Error. context.Canceled
// is treated like any other error (no special-case).
//
// Steps that the scheduler settles inline (Skipped or Canceled by their
// Condition) bypass the interceptor chain entirely and produce no span.
func NewAttemptInterceptor(opts ...Option) flow.AttemptInterceptor {
	cfg := newConfig(opts)
	tracer := cfg.resolveTracer()
	return flow.AttemptInterceptorFunc(func(ctx context.Context, step flow.Steper, attempt uint64, next func(context.Context) error) error {
		spanName := fmt.Sprintf("%s (attempt %d)", flow.String(step), attempt)
		if cfg.attemptSpanNamer != nil {
			spanName = cfg.attemptSpanNamer(step, attempt)
		}
		// User attributes first, canonical defaults last so OTel's
		// last-write-wins semantics keep canonical attrs authoritative.
		attrs := make([]attribute.KeyValue, 0, 4)
		if cfg.attemptAttributes != nil {
			attrs = append(attrs, cfg.attemptAttributes(step, attempt)...)
		}
		attrs = append(attrs,
			attribute.String(attrStepName, flow.String(step)),
			attribute.Int64(attrStepAttempt, int64(attempt)),
		)
		ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
		defer span.End()

		err := next(ctx)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		return err
	})
}
