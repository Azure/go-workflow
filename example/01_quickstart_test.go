package flow_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"

	flow "github.com/Azure/go-workflow"
)

// # Quickstart: a 3-minute tour of go-workflow
//
// **What you'll learn**
//   - A Workflow is a DAG of Steps; Steps with no path between them run in parallel.
//   - Pass data between Steps via typed Input / Output.
//   - One call to Workflow.Do executes the whole graph.
//
// **The scenario**
//
// Build a user profile that is the union of two pieces of data fetched from
// independent endpoints:
//
//	    ┌── FetchUser ──┐
//	    │               │
//	  start             ├──► BuildProfile ──► (result)
//	    │               │
//	    └── FetchPosts ─┘
//
// FetchUser and FetchPosts have no dependency on each other so the Workflow
// runs them concurrently. BuildProfile waits until both are done, then
// reads their Output via the typed Input callback.
//
// Open the next file (02_steps_and_deps_test.go) to dig into how dependencies
// are declared.
func ExampleWorkflow_quickstart() {
	// httptest stand-ins for two real services. In a real program these
	// would be remote HTTP calls; the rest of the file works the same way.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "Alice"})
		case "/posts":
			_ = json.NewEncoder(w).Encode([]string{"hello", "world"})
		}
	}))
	defer server.Close()

	var (
		// Two independent fetchers. FuncO declares "no input, typed output".
		fetchUser = flow.FuncO("FetchUser", func(ctx context.Context) (string, error) {
			return getJSON[map[string]string](ctx, server.URL+"/user")["name"], nil
		})
		fetchPosts = flow.FuncO("FetchPosts", func(ctx context.Context) ([]string, error) {
			return getJSON[[]string](ctx, server.URL+"/posts"), nil
		})

		// Downstream step. profileInput is the typed input we'll fill from
		// the two upstreams' outputs.
		buildProfile = flow.FuncI("BuildProfile", func(ctx context.Context, in profileInput) error {
			sort.Strings(in.Posts) // map iteration is unordered; stable output for the godoc check.
			fmt.Printf("%s has %d posts: %v\n", in.Name, len(in.Posts), in.Posts)
			return nil
		})
	)

	w := new(flow.Workflow)
	w.Add(
		// Wire the dependency and supply the Input callback in one shot.
		// Input runs at runtime, after all upstreams have terminated, so
		// fetchUser.Output and fetchPosts.Output are ready to read.
		flow.Step(buildProfile).
			DependsOn(fetchUser, fetchPosts).
			Input(func(ctx context.Context, f *flow.Function[profileInput, struct{}]) error {
				f.Input = profileInput{Name: fetchUser.Output, Posts: fetchPosts.Output}
				return nil
			}),
	)

	if err := w.Do(context.Background()); err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// Alice has 2 posts: [hello world]
}

// profileInput is the typed Input for buildProfile. Lifting it to a named
// type keeps the Step / Input / Function generics readable.
type profileInput struct {
	Name  string
	Posts []string
}

// getJSON is a small test helper. Real code would handle errors properly;
// this is a quickstart, not an HTTP tutorial.
func getJSON[T any](ctx context.Context, url string) T {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var out T
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}
