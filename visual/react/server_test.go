package react_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	flow "github.com/Azure/go-workflow"
	"github.com/Azure/go-workflow/visual/react"
	"github.com/stretchr/testify/assert"
)

func TestStaticHandler(t *testing.T) {
	var (
		a  = flow.NoOp("A")
		b  = flow.NoOp("B")
		c  = flow.NoOp("C")
		ab = new(flow.Workflow).Add(
			flow.Step(b).DependsOn(a),
		)
		abc = new(flow.Workflow).Add(
			flow.Step(c).DependsOn(ab),
		)
	)
	sh := &react.StaticHandler{Workflow: abc}

	req, err := http.NewRequest("GET", "/", nil)
	assert.NoError(t, err)

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(sh.ServeHTTP)

	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	var root react.Node
	assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &root))
	assert.Equal(t, "root", root.ID)
	if assert.Len(t, root.Children, 2) {
		for _, child := range root.Children {
			if len(child.Children) == 0 {
				assert.Equal(t, "C", child.Labels[0].Text)
			} else {
				if assert.Len(t, child.Children, 2) {
					for _, grandchild := range child.Children {
						assert.Contains(t, []string{"A", "B"}, grandchild.Labels[0].Text)
					}
				}
			}
		}
	}
}
