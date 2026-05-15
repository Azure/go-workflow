package flowotel

import (
	"context"

	flow "github.com/Azure/go-workflow"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// NewStepInterceptor returns a flow.StepInterceptor that emits one
// OpenTelemetry span per Step lifetime (covering all retry attempts).
//
// The span name defaults to flow.String(step) and may be overridden via
// WithStepSpanNamer. The default attribute set always includes
// workflow.step.name = flow.String(step) and, after next() returns,
// workflow.step.status ∈ {"success", "error"}. Extra attributes can be
// supplied via WithStepAttributes; they are appended to (not in place of)
// the defaults at span-start time. Canonical attributes (workflow.step.name,
// workflow.step.status) always win over user-supplied attributes — i.e.,
// WithStepAttributes cannot override them.
//
// On a non-nil error from next() the span records the error via
// span.RecordError and sets its status to codes.Error. context.Canceled
// is treated like any other error (no special-case).
//
// Steps that the scheduler settles inline (Skipped or Canceled by their
// Condition) bypass the interceptor chain entirely and produce no span.
func NewStepInterceptor(opts ...Option) flow.StepInterceptor {
	cfg := newConfig(opts)
	tracer := cfg.resolveTracer()
	return flow.StepInterceptorFunc(func(ctx context.Context, step flow.Steper, next func(context.Context) error) error {
		spanName := flow.String(step)
		if cfg.stepSpanNamer != nil {
			spanName = cfg.stepSpanNamer(step)
		}
		// User attributes first, canonical defaults last so OTel's
		// last-write-wins semantics keep canonical attrs authoritative.
		attrs := make([]attribute.KeyValue, 0, 4)
		if cfg.stepAttributes != nil {
			attrs = append(attrs, cfg.stepAttributes(step)...)
		}
		attrs = append(attrs, attribute.String(attrStepName, flow.String(step)))
		ctx, span := tracer.Start(ctx, spanName, trace.WithAttributes(attrs...))
		defer span.End()

		err := next(ctx)
		if err != nil {
			span.SetAttributes(attribute.String(attrStepStatus, statusError))
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		} else {
			span.SetAttributes(attribute.String(attrStepStatus, statusSuccess))
		}
		return err
	})
}
