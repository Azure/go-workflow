package azure_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
	"github.com/Azure/go-workflow/handler/azure"
	"github.com/stretchr/testify/assert"
)

type TestTransporter func(*http.Request) (*http.Response, error)

func (t TestTransporter) Do(req *http.Request) (*http.Response, error) {
	return t(req)
}

func NewTestPipeline(t *testing.T, opt *policy.ClientOptions) runtime.Pipeline {
	return runtime.NewPipeline("test", "test", runtime.PipelineOptions{}, opt)
}

func TestSetHeadersPolicy(t *testing.T) {
	cases := map[string]struct {
		base   func(http.Header)
		policy azure.PolicySetHeaders
		expect func(*testing.T, http.Header)
	}{
		"should set if missing": {
			base: nil,
			policy: azure.PolicySetHeaders{
				SetIfMissing: true,
				Headers: map[string]string{
					"key": "value",
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.Equal(t, "value", h.Get("key"))
			},
		},
		"should merge existing": {
			base: func(h http.Header) {
				h.Add("key", "value1")
				h.Add("key", "value2")
			},
			policy: azure.PolicySetHeaders{
				MergeExisting: true,
				Headers: map[string]string{
					"key": "value",
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.ElementsMatch(t, []string{"value1", "value2", "value"}, h.Values("key"))
			},
		},
		"merge existing is idempotent": {
			base: func(h http.Header) {
				h.Add("key", "value1")
				h.Add("key", "value2")
			},
			policy: azure.PolicySetHeaders{
				SetIfMissing:  true,
				MergeExisting: true,
				Headers: map[string]string{
					"key": "value2",
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.ElementsMatch(t, []string{"value1", "value2"}, h.Values("key"))
			},
		},
		"not merge existing will replace": {
			base: func(h http.Header) {
				h.Add("key", "value1")
				h.Add("key", "value2")
			},
			policy: azure.PolicySetHeaders{
				MergeExisting: false,
				Headers: map[string]string{
					"key": "value",
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.Equal(t, "value", h.Get("key"))
			},
		},
		"not set if exist": {
			base: func(h http.Header) {
				h.Add("key", "value1")
				h.Add("key", "value2")
			},
			policy: azure.PolicySetHeaders{
				SetIfMissing:  false,
				MergeExisting: false,
				Headers: map[string]string{
					"key": "value",
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.Equal(t, "value", h.Get("key"))
			},
		},
		"headers slice will add multiple": {
			base: func(h http.Header) {
				h.Add("key", "value1")
			},
			policy: azure.PolicySetHeaders{
				MergeExisting: true,
				HeadersSlice: map[string][]string{
					"key": {"value2", "value3"},
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.ElementsMatch(t, []string{"value1", "value2", "value3"}, h.Values("key"))
			},
		},
		"header slice set if missing": {
			base: func(h http.Header) {
				h.Add("key", "value1")
			},
			policy: azure.PolicySetHeaders{
				SetIfMissing: true,
				HeadersSlice: map[string][]string{
					"key": {"value2", "value3"},
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.Equal(t, "value1", h.Get("key"))
			},
		},
		"header slice not merge existing will replace": {
			base: func(h http.Header) {
				h.Add("key", "value1")
			},
			policy: azure.PolicySetHeaders{
				MergeExisting: false,
				HeadersSlice: map[string][]string{
					"key": {"value2", "value3"},
				},
			},
			expect: func(t *testing.T, h http.Header) {
				assert.ElementsMatch(t, []string{"value2", "value3"}, h.Values("key"))
			},
		},
	}
	for name, c := range cases {
		c := c
		t.Run(name, func(t *testing.T) {
			req, err := runtime.NewRequest(context.Background(), http.MethodGet, "http://example.com")
			assert.NoError(t, err)
			header := make(http.Header)
			req.Raw().Header = header
			if c.base != nil {
				c.base(header)
			}
			resp, err := c.policy.Do(req)
			assert.Nil(t, resp)
			assert.ErrorContains(t, err, "no more policies")
			c.expect(t, header)
		})
	}
}

func TestPolicyGetResponse(t *testing.T) {
	t.Run("should get response", func(t *testing.T) {
		p := new(azure.PolicyGetResponse)

		pipe := NewTestPipeline(t, &policy.ClientOptions{
			Transport: TestTransporter(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("Response Body")),
				}, nil
			}),
			PerCallPolicies: []policy.Policy{p},
		})
		req, err := runtime.NewRequest(context.Background(), http.MethodGet, "http://example.com")
		assert.NoError(t, err)

		got, err := pipe.Do(req)
		assert.NoError(t, err)
		assert.NotNil(t, got)
		assert.NotNil(t, p.Response)
		assert.Equal(t, http.StatusOK, got.StatusCode)
		assert.Equal(t, http.StatusOK, p.Response.StatusCode)

		gotBody, err := io.ReadAll(got.Body)
		assert.NoError(t, err)
		assert.Equal(t, "Response Body", string(gotBody))
		pBody, err := io.ReadAll(p.Response.Body)
		assert.NoError(t, err)
		assert.Equal(t, "Response Body", string(pBody))
	})
}
