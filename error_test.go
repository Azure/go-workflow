package flow

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecorateErrorJSON(t *testing.T) {
	someError := errors.New("some error")
	t.Run("plain error will have a error field", func(t *testing.T) {
		j, err := json.Marshal(StatusError{
			Status: Failed,
			Err:    someError,
		})
		assert.NoError(t, err)
		assert.JSONEq(t, `{"status":"Failed","error":"some error"}`, string(j))
	})
	t.Run("error with MarshalJSON will be called", func(t *testing.T) {
		j, err := json.Marshal(StatusError{
			Status: Failed,
			Err:    WithStackTraces(3, 32)(someError),
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
		j, err := json.Marshal(StatusError{
			Status: Skipped,
			Err:    Skip(someError),
		})
		assert.NoError(t, err)
		assert.JSONEq(t, `{"status":"Skipped","error":"some error"}`, string(j))
		j, err = json.Marshal(StatusError{
			Status: Failed,
			Err:    WithStackTraces(3, 32)(someError),
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
	errCycleDependency := ErrCycleDependency{
		&fakeStep{Name: "step2"}: {
			&fakeStep{Name: "step1"},
		},
	}
	assert.Equal(t, "Cycle Dependency Error:\n*flow.fakeStep(&{step2}): [*flow.fakeStep(&{step1})]", errCycleDependency.Error())
}
