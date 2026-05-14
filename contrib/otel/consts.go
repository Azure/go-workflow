package otel

// Attribute keys and status values emitted by the contrib/otel interceptors.
const (
	attrStepName    = "workflow.step.name"
	attrStepStatus  = "workflow.step.status"
	attrStepAttempt = "workflow.step.attempt"

	statusSuccess = "success"
	statusError   = "error"
)
