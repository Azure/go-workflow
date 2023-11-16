package azure_test

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/go-workflow/handler/azure"
	"github.com/stretchr/testify/assert"
)

func TestClientOptions(t *testing.T) {
	t.Run("should return default options if nil", func(t *testing.T) {
		c := new(azure.Context)
		opt := c.ClientOptions(nil)
		assert.NotNil(t, opt)
		assert.Equal(t, cloud.AzurePublic, opt.Cloud)
		assert.True(t, opt.DisableRPRegistration)
	})
}

func TestAddClientOptions(t *testing.T) {
	t.Run("should assign if nil", func(t *testing.T) {
		co := new(azure.CredOpt)
		co.AddClientOptions(func(co *arm.ClientOptions) *arm.ClientOptions {
			co.APIVersion = "test"
			return co
		})
		assert.NotNil(t, co.ClientOptions)
		var opt arm.ClientOptions
		got := co.ClientOptions(&opt)
		assert.NotNil(t, got)
		assert.Equal(t, "test", got.APIVersion)
	})
	t.Run("should add options", func(t *testing.T) {
		co := new(azure.CredOpt)
		co.ClientOptions = func(co *arm.ClientOptions) *arm.ClientOptions {
			co.APIVersion = "test"
			return co
		}
		co.AddClientOptions(func(co *arm.ClientOptions) *arm.ClientOptions {
			co.APIVersion += "2"
			return co
		})
		assert.NotNil(t, co.ClientOptions)
		var opt arm.ClientOptions
		got := co.ClientOptions(&opt)
		assert.NotNil(t, got)
		assert.Equal(t, "test2", got.APIVersion)
	})
}
