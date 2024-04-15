package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	flow "github.com/Azure/go-workflow"
)

type StaticHandler struct {
	Workflow *flow.Workflow
}

func (sh StaticHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	root := &Node{ID: "root"}
	nodes := map[string]*Node{nodeID(sh.Workflow): root}
	getNode := func(s flow.Steper) *Node {
		id := nodeID(s)
		node, ok := nodes[id]
		if !ok {
			node = &Node{ID: id}
			nodes[id] = node
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
					eid := edgeID(up, r)
					getNode(parent).Edges = append(getNode(parent).Edges, &Edge{
						ID:      eid,
						Sources: []string{nodeID(up)},
						Targets: []string{nodeID(r)},
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

func nodeID(s flow.Steper) string    { return fmt.Sprintf("%p", s) }
func edgeID(s, t flow.Steper) string { return fmt.Sprintf("%p-%p", s, t) }

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
