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
	resp.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
	nodes := map[string]Node{}
	edges := map[string]Edge{}
	flow.Traverse(sh.Workflow, func(s flow.Steper, walked []flow.Steper) flow.TraverseDecision {
		if w, ok := s.(interface {
			Unwrap() []flow.Steper
			UpstreamOf(flow.Steper) map[flow.Steper]flow.StepResult
		}); ok {
			for _, r := range w.Unwrap() {
				id := nodeID(r)
				n := Node{
					ID:     id,
					Name:   flow.String(r),
					ZIndex: len(walked),
				}
				for i := len(walked) - 1; i >= 0; i-- {
					if _, ok := walked[i].(interface{ Unwrap() []flow.Steper }); ok {
						if i == len(walked)-1 {
							n.ParentID = nodeID(s)
							break
						} else if i < len(walked)-1 {
							n.ParentID = nodeID(walked[i+1])
							break
						}
					}
				}
				nodes[id] = n

				for up := range w.UpstreamOf(r) {
					eid := edgeID(up, r)
					edges[eid] = Edge{
						ID:     eid,
						Source: nodeID(up),
						Target: nodeID(r),
						ZIndex: max(nodes[nodeID(up)].ZIndex, nodes[nodeID(r)].ZIndex),
					}
				}
			}
		}
		return flow.TraverseDecision{Continue: true}
	})
	b, err := json.Marshal(Flow{
		Nodes: flow.Values(nodes),
		Edges: flow.Values(edges),
	})
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

type Flow struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId"`
	ZIndex   int    `json:"zIndex"`
}

type Edge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	ZIndex int    `json:"zIndex"`
}
