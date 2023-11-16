package azure

import (
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/go-workflow/flcore"
)

var (
	FromContext = flcore.FromContext[Ctx]
	NewContext  = flcore.NewContext
)

type Ctx interface {
	flcore.Logger
	Azure() *Context
}

// Context provides contextual information an azure handler needs
type Context struct {
	Cloud    cloud.Configuration
	Region   string
	TenantID string

	SubscriptionID string

	ClientID     string
	ClientSecret flcore.RedactedString

	TokenCredential azcore.TokenCredential
}
