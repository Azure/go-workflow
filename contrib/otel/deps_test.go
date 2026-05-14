// Anchors test-only OpenTelemetry SDK dependencies in go.mod / go.sum so the
// dependency policy for the contrib/otel module (SDK is test-only) is observable
// from the bootstrap commit alone. Real tests in later tasks import these
// packages directly; remove this file once they do.
package otel_test

import (
	_ "go.opentelemetry.io/otel/sdk/trace"
	_ "go.opentelemetry.io/otel/sdk/trace/tracetest"
)
