// Package otel provides OpenTelemetry tracing integration for go-workflow.
//
// It plugs into the two interceptor extension points exposed by go-workflow:
// StepInterceptor (one span per Step lifetime, across all retries) and
// AttemptInterceptor (one span per individual attempt). Use both together
// for a parent/child structure, or just one if you want a flatter trace.
//
// # Usage
//
//	import (
//	    flow "github.com/Azure/go-workflow"
//	    "github.com/Azure/go-workflow/contrib/otel"
//	)
//
//	w := &flow.Workflow{
//	    Option: flow.WorkflowOption{
//	        StepInterceptors: []flow.StepInterceptor{
//	            flowotel.NewStepInterceptor(flowotel.WithTracerProvider(tp)),
//	        },
//	        AttemptInterceptors: []flow.AttemptInterceptor{
//	            flowotel.NewAttemptInterceptor(flowotel.WithTracerProvider(tp)),
//	        },
//	    },
//	}
//
// See the runnable Example for a complete wiring with a stdout exporter.
//
// # Span conventions
//
// Step spans are named flow.String(step) and carry attributes
// workflow.step.name and workflow.step.status ("success" or "error").
// Attempt spans are named "<step> (attempt N)" and carry workflow.step.name
// and workflow.step.attempt (int64).
//
// All defaults can be overridden via Options (WithStepSpanNamer,
// WithAttemptSpanNamer, WithStepAttributes, WithAttemptAttributes).
// Canonical attributes (workflow.step.name, workflow.step.status,
// workflow.step.attempt) always win — user-supplied keys with the same
// names are silently superseded.
//
// # Parent/child relation
//
// When both interceptors are registered, attempt spans are children of the
// step span (same TraceID, Parent.SpanID equals the step span's SpanID).
// When only one is registered, that span is a child of whatever span (if
// any) is on the caller-provided context.
//
// # Provider resolution
//
// WithTracerProvider sets the OpenTelemetry TracerProvider explicitly.
// If unset (or nil), each factory falls back to otel.GetTracerProvider()
// at the moment NewStepInterceptor / NewAttemptInterceptor is called —
// not lazily on every interception. Swapping the global provider after
// construction does not affect already-built interceptors.
//
// # Skipped and Canceled-by-Condition steps
//
// Steps whose Condition resolves to Skipped or Canceled are settled inline
// by the workflow scheduler and bypass the interceptor chain entirely. As
// a result, they produce zero spans through this package. If you need to
// observe terminal-by-condition statuses, watch the StepResult instead.
//
// # context.Canceled
//
// When the error returned by next() satisfies errors.Is(err, context.Canceled),
// the resulting span is ended with RecordError + SetStatus(codes.Error) —
// the same as any other error. There is no special-case suppression. Users
// running graceful shutdowns who want different semantics can wrap the
// returned interceptor.
package flowotel
