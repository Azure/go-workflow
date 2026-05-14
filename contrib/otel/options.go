package otel

import (
	flow "github.com/Azure/go-workflow"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// defaultTracerName is the default instrumentation name passed to
// TracerProvider.Tracer when WithTracerName is not used.
const defaultTracerName = "github.com/Azure/go-workflow/contrib/otel"

// config is the resolved configuration shared by both interceptor factories
// (NewStepInterceptor and NewAttemptInterceptor). The same Option values are
// accepted by both factories; options that target one layer are no-ops on the
// other.
type config struct {
	tracerProvider    trace.TracerProvider
	tracerName        string
	stepSpanNamer     func(flow.Steper) string
	attemptSpanNamer  func(flow.Steper, uint64) string
	stepAttributes    func(flow.Steper) []attribute.KeyValue
	attemptAttributes func(flow.Steper, uint64) []attribute.KeyValue
}

// Option configures a step or attempt interceptor produced by
// NewStepInterceptor / NewAttemptInterceptor. The same Option type is accepted
// by both factories; each option's documentation states which interceptor it
// affects (the other factory ignores it).
type Option func(*config)

// newConfig applies opts to a fresh config and returns it. nil options are
// skipped so callers can build slices conditionally.
func newConfig(opts []Option) *config {
	c := &config{}
	for _, o := range opts {
		if o != nil {
			o(c)
		}
	}
	return c
}

// WithTracerProvider sets the OpenTelemetry TracerProvider used to obtain the
// Tracer that emits step and attempt spans. When unset (or set to nil), each
// factory falls back to otel.GetTracerProvider() at the moment
// NewStepInterceptor / NewAttemptInterceptor is called (not lazily on every
// interception).
//
// Affects: NewStepInterceptor, NewAttemptInterceptor.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(c *config) { c.tracerProvider = tp }
}

// WithTracerName overrides the instrumentation name passed to
// TracerProvider.Tracer. Default:
// "github.com/Azure/go-workflow/contrib/otel".
//
// Affects: NewStepInterceptor, NewAttemptInterceptor.
func WithTracerName(name string) Option {
	return func(c *config) { c.tracerName = name }
}

// WithStepSpanNamer overrides the default step span name (flow.String(step))
// with a caller-supplied function. Passing a nil fn is a no-op and leaves the
// previously configured (or default) namer in place.
//
// Affects: NewStepInterceptor only. NewAttemptInterceptor ignores this option.
func WithStepSpanNamer(fn func(flow.Steper) string) Option {
	return func(c *config) {
		if fn != nil {
			c.stepSpanNamer = fn
		}
	}
}

// WithAttemptSpanNamer overrides the default attempt span name
// ("<step> (attempt N)") with a caller-supplied function. Passing a nil fn is
// a no-op and leaves the previously configured (or default) namer in place.
//
// Affects: NewAttemptInterceptor only. NewStepInterceptor ignores this option.
func WithAttemptSpanNamer(fn func(flow.Steper, uint64) string) Option {
	return func(c *config) {
		if fn != nil {
			c.attemptSpanNamer = fn
		}
	}
}

// WithStepAttributes registers a function that returns extra attributes to
// attach to step spans at span-start time. The returned attributes are added
// in addition to the defaults (e.g. workflow.step.name). Passing a nil fn is
// a no-op.
//
// Note: canonical attributes set by the interceptor (workflow.step.name,
// workflow.step.status) cannot be overridden by this option; passing those
// keys is silently superseded.
//
// Affects: NewStepInterceptor only. NewAttemptInterceptor ignores this option.
func WithStepAttributes(fn func(flow.Steper) []attribute.KeyValue) Option {
	return func(c *config) {
		if fn != nil {
			c.stepAttributes = fn
		}
	}
}

// WithAttemptAttributes registers a function that returns extra attributes to
// attach to attempt spans at span-start time. The returned attributes are
// added in addition to the defaults (e.g. workflow.step.name,
// workflow.step.attempt). Passing a nil fn is a no-op.
//
// Note: canonical attributes set by the interceptor (workflow.step.name,
// workflow.step.attempt) cannot be overridden by this option; passing those
// keys is silently superseded.
//
// Affects: NewAttemptInterceptor only. NewStepInterceptor ignores this option.
func WithAttemptAttributes(fn func(flow.Steper, uint64) []attribute.KeyValue) Option {
	return func(c *config) {
		if fn != nil {
			c.attemptAttributes = fn
		}
	}
}

// resolveTracer picks the configured TracerProvider (falling back to the
// global provider via otel.GetTracerProvider when nil) and returns a Tracer
// with the configured (or default) instrumentation name. It is intended to be
// called once per factory invocation at construction time; the resulting
// Tracer is captured by the returned interceptor closure.
func (c *config) resolveTracer() trace.Tracer {
	tp := c.tracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	name := c.tracerName
	if name == "" {
		name = defaultTracerName
	}
	return tp.Tracer(name)
}
