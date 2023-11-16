package azure

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/go-workflow/flcore/to"
)

var DefaultPerCallPolicies = []policy.Policy{
	new(PolicyLogRequest),
	new(PolicyLogResponse),
}

func (c *Context) ClientOptions(opt *arm.ClientOptions) *arm.ClientOptions {
	if opt == nil {
		opt = new(arm.ClientOptions)
	}
	if opt.Cloud.Services == nil {
		opt.Cloud = cloud.AzurePublic
	}
	opt.DisableRPRegistration = true
	opt.PerCallPolicies = append(opt.PerCallPolicies, DefaultPerCallPolicies...)
	return opt
}

// CredOpt = TokenCredential + ClientOptions
// Handlers can use CredOpt in input struct to get customized TokenCredential and ClientOptions from Handler users.
type CredOpt struct {
	azcore.TokenCredential
	ClientOptions to.OverrideFunc[*arm.ClientOptions]
}

func (co *CredOpt) AddClientOptions(newOpt to.OverrideFunc[*arm.ClientOptions]) {
	if oldOpt := co.ClientOptions; oldOpt == nil {
		co.ClientOptions = newOpt
	} else {
		co.ClientOptions = func(o *arm.ClientOptions) *arm.ClientOptions {
			return newOpt(oldOpt(o))
		}
	}
}
