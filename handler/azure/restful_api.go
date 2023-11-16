package azure

import (
	"context"
	"fmt"
	"net/http"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	flow "github.com/Azure/go-workflow"
	"github.com/Azure/go-workflow/flcore/to"
)

const (
	moduleName    = "flow/azure"
	moduleVersion = "v0.0.1"
)

var (
	DefaultRestfulAPIApiVersion = "2023-07-02-preview"
)

type RestfulAPI struct {
	flow.Base
	// Input
	Method     string
	UrlPath    string
	ApiVersion string
	Request    func(*policy.Request) // modify the request before sending
	CredOpt
	// Output
	Response *http.Response
}

func (r *RestfulAPI) String() string {
	if r.StepName != "" {
		return r.StepName
	}
	return fmt.Sprintf("RestfulAPI(%s %s?api-version=%s)", r.Method, r.UrlPath,
		to.Coalesce(r.ApiVersion, DefaultRestfulAPIApiVersion))
}

func (r *RestfulAPI) Do(ctx context.Context) error {
	tc := FromContext(ctx)
	if r.ApiVersion == "" {
		r.ApiVersion = DefaultRestfulAPIApiVersion
	}
	client, err := arm.NewClient(
		moduleName+"."+"RestfulAPI",
		moduleVersion,
		to.Coalesce(r.TokenCredential, tc.Azure().TokenCredential),
		to.Override(tc.Azure().ClientOptions, r.ClientOptions)(nil),
	)
	if err != nil {
		return err
	}
	req, err := r.prepareRequest(ctx, client)
	if err != nil {
		return err
	}
	resp, err := client.Pipeline().Do(req)
	if err != nil {
		return err
	}
	r.Response = resp
	return nil
}

func (r *RestfulAPI) prepareRequest(ctx context.Context, client *arm.Client) (*policy.Request, error) {
	req, err := runtime.NewRequest(ctx, r.Method, runtime.JoinPaths(client.Endpoint(), r.UrlPath))
	if err != nil {
		return nil, err
	}
	reqQP := req.Raw().URL.Query()
	reqQP.Set("api-version", r.ApiVersion)
	req.Raw().URL.RawQuery = reqQP.Encode()
	req.Raw().Header.Set("Accept", "application/json")
	if r.Request != nil {
		r.Request(req)
	}
	return req, nil
}
