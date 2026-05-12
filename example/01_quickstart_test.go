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
//   - Any struct of yours becomes a Step by adding one method:
//     `Do(context.Context) error`. No interface to embed, no generics, no
//     decorators. Your domain types ARE the workflow.
//   - A Workflow is a DAG of Steps; Steps with no path between them run
//     in parallel.
//   - Use `Input` to flow data from upstream Steps into a downstream
//     Step's fields, right before its `Do` runs.
//
// **The scenario**
//
// Build a user profile that combines two pieces of data fetched from
// independent endpoints:
//
//	    ┌── FetchUser ──┐
//	    │               │
//	  start             ├──► BuildProfile ──► (result)
//	    │               │
//	    └── FetchPosts ─┘
//
// `FetchUser` and `FetchPosts` have no dependency on each other so the
// Workflow runs them concurrently. `BuildProfile` waits until both are
// done; an `Input` callback then copies their outputs into BuildProfile's
// own fields, and `Do` reads them.
//
// Read on for 02_steps_and_deps_test.go to see more wiring shapes.
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

	// Construct the steps. Each one is just a value of our own struct type.
	// Configuration goes in via the constructor; results come out via fields.
	user := &FetchUser{BaseURL: server.URL}
	posts := &FetchPosts{BaseURL: server.URL}
	profile := &BuildProfile{}

	// Wire the graph and the data flow in one go. Input(fn) registers fn
	// to run after every upstream has terminated and before profile.Do —
	// so user.Name and posts.Posts are safe to read inside fn.
	w := new(flow.Workflow)
	w.Add(
		flow.Step(profile).
			DependsOn(user, posts).
			Input(func(ctx context.Context, p *BuildProfile) error {
				p.Name = user.Name
				p.Posts = posts.Posts
				return nil
			}),
	)

	if err := w.Do(context.Background()); err != nil {
		fmt.Println("error:", err)
	}
	// Output:
	// Alice has 2 posts: [hello world]
}

// FetchUser is a Step. The struct holds its configuration (BaseURL) and
// publishes its result (Name) — both as plain exported fields. There is
// nothing magic about it: any type with a Do(context.Context) error
// method satisfies flow.Steper.
type FetchUser struct {
	BaseURL string // input: configured at construction time
	Name    string // output: filled in by Do
}

func (f *FetchUser) Do(ctx context.Context) error {
	var body map[string]string
	if err := getJSON(ctx, f.BaseURL+"/user", &body); err != nil {
		return err
	}
	f.Name = body["name"]
	return nil
}

// FetchPosts is another Step. Same shape — a struct with config-in,
// result-out, and a Do method.
type FetchPosts struct {
	BaseURL string
	Posts   []string
}

func (f *FetchPosts) Do(ctx context.Context) error {
	return getJSON(ctx, f.BaseURL+"/posts", &f.Posts)
}

// BuildProfile is the downstream Step. Its inputs (Name, Posts) are
// plain fields populated by the Input callback at wiring time — see
// the Input(...) call in ExampleWorkflow_quickstart above. Do then just
// reads those fields.
//
// Keeping data on Step fields (rather than reaching into upstream
// objects from Do) makes BuildProfile self-contained: it can be tested
// in isolation by setting Name and Posts and calling Do directly.
type BuildProfile struct {
	Name  string   // input, filled by Input callback
	Posts []string // input, filled by Input callback
}

func (b *BuildProfile) Do(ctx context.Context) error {
	posts := append([]string(nil), b.Posts...)
	sort.Strings(posts) // map iteration is unordered upstream; pin the output for the godoc check.
	fmt.Printf("%s has %d posts: %v\n", b.Name, len(posts), posts)
	return nil
}

// getJSON is a small test helper. Real code would handle errors properly;
// this is a quickstart, not an HTTP tutorial.
func getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}
