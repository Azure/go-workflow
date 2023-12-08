package flow

import "context"

// MockStep helps to mock a step.
// After building a workflow, you can mock the original step with a mock step.
type MockStep struct {
	Step   Steper
	MockDo func(context.Context) error
}

func (m *MockStep) Unwrap() Steper               { return m.Step }
func (m *MockStep) Do(ctx context.Context) error { return m.MockDo(ctx) }

type MockSteps struct {
	Steps  []Steper
	MockDo func(context.Context) error
}

func (m *MockSteps) Unwrap() []Steper             { return m.Steps }
func (m *MockSteps) Do(ctx context.Context) error { return m.MockDo(ctx) }
