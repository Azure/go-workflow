module github.com/Azure/go-workflow/contrib/otel

go 1.23.0

require (
	github.com/Azure/go-workflow v0.0.0-00010101000000-000000000000
	go.opentelemetry.io/otel v1.37.0
	go.opentelemetry.io/otel/sdk v1.37.0
	go.opentelemetry.io/otel/trace v1.37.0
)

require (
	github.com/benbjohnson/clock v1.3.5 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	go.opentelemetry.io/auto/sdk v1.1.0 // indirect
	go.opentelemetry.io/otel/metric v1.37.0 // indirect
	golang.org/x/sys v0.34.0 // indirect
)

replace github.com/Azure/go-workflow => ../..
