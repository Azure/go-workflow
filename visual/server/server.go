package server

import (
	"encoding/json"
	"log/slog"
	"net/http"

	flow "github.com/Azure/go-workflow"
	"github.com/google/uuid"
)

type StaticHandler struct {
	Workflow *flow.Workflow
}

func (sh StaticHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	resp.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	root := &Node{ID: "root"}
	nodes := map[flow.Steper]*Node{sh.Workflow: root}
	getNode := func(s flow.Steper) *Node {
		node, ok := nodes[s]
		if !ok {
			node = &Node{ID: uuid.NewString()}
			nodes[s] = node
		}
		return node
	}
	flow.Traverse(sh.Workflow, func(s flow.Steper, walked []flow.Steper) flow.TraverseDecision {
		if w, ok := s.(interface {
			Unwrap() []flow.Steper
			UpstreamOf(flow.Steper) map[flow.Steper]flow.StepResult
		}); ok {
			for _, r := range w.Unwrap() {
				n := getNode(r)
				n.Labels = append(n.Labels, Label{flow.String(r)})
				parent := s
				for i := len(walked) - 1; i >= 0; i-- {
					if _, ok := walked[i].(interface{ Unwrap() []flow.Steper }); ok {
						if i < len(walked)-1 {
							parent = walked[i+1]
							break
						}
					}
				}
				getNode(parent).Children = append(getNode(parent).Children, n)

				for up := range w.UpstreamOf(r) {
					eid := uuid.NewString()
					getNode(parent).Edges = append(getNode(parent).Edges, &Edge{
						ID:      eid,
						Sources: []string{getNode(up).ID},
						Targets: []string{getNode(r).ID},
					})
				}
			}
		}
		return flow.TraverseDecision{Continue: true}
	})
	b, err := json.Marshal(root)
	if err != nil {
		resp.WriteHeader(http.StatusInternalServerError)
		slog.Error("failed to marshal flow", "error", err)
		return
	}
	resp.WriteHeader(http.StatusOK)
	if _, err := resp.Write(b); err != nil {
		slog.Error("failed to write response", "error", err)
		return
	}
}

type Node struct {
	ID       string  `json:"id"`
	Children []*Node `json:"children,omitempty"`
	Edges    []*Edge `json:"edges,omitempty"`
	X        int     `json:"x,omitempty"`
	Y        int     `json:"y,omitempty"`
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Labels   []Label `json:"labels,omitempty"`
}
type Label struct {
	Text string `json:"text,omitempty"`
}

type Edge struct {
	ID      string   `json:"id"`
	Sources []string `json:"sources"`
	Targets []string `json:"targets"`
}
