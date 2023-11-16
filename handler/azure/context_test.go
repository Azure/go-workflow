package azure_test

import (
	"context"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/go-workflow/flcore"
	"github.com/Azure/go-workflow/handler/azure"
)

type TestContext struct {
	flcore.Logger
	azure.Context
}

func (tc *TestContext) Azure() *azure.Context { return &tc.Context }

func NewTestContext(t *testing.T) (context.Context, *TestContext) {
	tc := &TestContext{
		Logger: flcore.NewTestLogger(t),
		Context: azure.Context{
			Cloud:          cloud.AzurePublic,
			Region:         "eastus",
			TenantID:       "00000000-0000-0000-0000-000000000000",
			SubscriptionID: "00000000-0000-0000-0000-000000000000",
			ClientID:       "00000000-0000-0000-0000-000000000000",
			ClientSecret:   flcore.RedactedString("SUPER_SECRET"),
		},
	}
	return azure.NewContext(context.Background(), tc), tc
}
