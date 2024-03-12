package flow_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	flow "github.com/Azure/go-workflow"
	"github.com/stretchr/testify/assert"
)

func TestDecorateErrorJSON(t *testing.T) {
	someError := errors.New("some error")
	t.Run("plain error will have a error field", func(t *testing.T) {
		j, err := json.Marshal(flow.StatusError{
			Status: flow.Failed,
			Err:    someError,
		})
		assert.NoError(t, err)
		assert.JSONEq(t, `{"status":"Failed","error":"some error"}`, string(j))
	})
	t.Run("error with MarshalJSON will be called", func(t *testing.T) {
		j, err := json.Marshal(flow.StatusError{
			Status: flow.Failed,
			Err:    flow.WithStackTraces(3, 32)(someError),
		})
		assert.NoError(t, err)
		m := map[string]interface{}{}
		if assert.NoError(t, json.Unmarshal(j, &m)) {
			assert.Equal(t, "Failed", m["status"])
			assert.Equal(t, "some error", m["error"])
			assert.Less(t, 0, len(m["stack_traces"].([]interface{})))
		}
	})
	t.Run("skip cancel errors are transparent", func(t *testing.T) {
		j, err := json.Marshal(flow.StatusError{
			Status: flow.Skipped,
			Err:    flow.Skip(someError),
		})
		assert.NoError(t, err)
		assert.JSONEq(t, `{"status":"Skipped","error":"some error"}`, string(j))
		j, err = json.Marshal(flow.StatusError{
			Status: flow.Failed,
			Err:    flow.WithStackTraces(3, 32)(someError),
		})
		assert.NoError(t, err)
		m := map[string]interface{}{}
		if assert.NoError(t, json.Unmarshal(j, &m)) {
			assert.Equal(t, "Failed", m["status"])
			assert.Equal(t, "some error", m["error"])
			assert.Less(t, 0, len(m["stack_traces"].([]interface{})))
		}
	})
}

func TestErrCycleDependency(t *testing.T) {
	w := new(flow.Workflow).Add(
		flow.Step(succeededStep).DependsOn(succeededStep),
	)
	var errCycle flow.ErrCycleDependency
	if assert.ErrorAs(t, w.Do(context.Background()), &errCycle) {
		assert.ErrorContains(t, errCycle, "Succeeded: [Succeeded]")
	}
}
