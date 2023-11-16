package azure

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/go-workflow/flcore"
	"github.com/Azure/go-workflow/flcore/to"
)

type PolicyFunc func(*policy.Request) (*http.Response, error)

// Do implements the Policy interface on PolicyFunc.
func (pf PolicyFunc) Do(req *policy.Request) (*http.Response, error) {
	return pf(req)
}

// PolicySetHeaders sets http headers respecting the setting.
type PolicySetHeaders struct {
	SetIfMissing  bool // If SetIfMissing is true, the header will be set only if it hasn't set.
	MergeExisting bool // If MergeExisting is true, the header will be added into the existing header.
	Headers       map[string]string
	HeadersSlice  map[string][]string // HeaderSlice is to set multiple values for a header at same time.
}

func (p PolicySetHeaders) Do(req *policy.Request) (*http.Response, error) {
	header := req.Raw().Header
	for k, v := range p.Headers {
		if p.SetIfMissing && header.Get(k) != "" {
			continue
		}
		if p.MergeExisting {
			header.Add(k, v)
		} else {
			header.Set(k, v)
		}
	}
	for k, vs := range p.HeadersSlice {
		if p.SetIfMissing && header.Get(k) != "" {
			continue
		}
		if !p.MergeExisting {
			header.Del(k)
		}
		for _, v := range vs {
			header.Add(k, v)
		}
	}
	return req.Next()
}

// PolicyGetResponse gets the http response and tees body
type PolicyGetResponse struct {
	*http.Response
}

func (p *PolicyGetResponse) Do(req *policy.Request) (*http.Response, error) {
	resp, err := req.Next()
	if err != nil || resp == nil {
		return resp, err
	}
	p.Response = to.Ptr(*resp)
	var drainBodyErr error
	p.Response.Body, resp.Body, drainBodyErr = drainBody(resp.Body)
	return resp, errors.Join(err, drainBodyErr)
}

// COPY FROM net/http/httputil
//
// drainBody reads all of b to memory and then returns two equivalent
// ReadClosers yielding the same bytes.
//
// It returns an error if the initial slurp of all bytes fails. It does not attempt
// to make the returned ReadClosers have identical error-matching behavior.
func drainBody(b io.ReadCloser) (r1, r2 io.ReadCloser, err error) {
	if b == nil || b == http.NoBody {
		// No copying needed. Preserve the magic sentinel meaning of NoBody.
		return http.NoBody, http.NoBody, nil
	}
	var buf bytes.Buffer
	if _, err = buf.ReadFrom(b); err != nil {
		return nil, b, err
	}
	if err = b.Close(); err != nil {
		return nil, b, err
	}
	return io.NopCloser(&buf), io.NopCloser(bytes.NewReader(buf.Bytes())), nil
}

var (
	DefaultLogRequestLevel = func(logger flcore.Logger) func(context.Context, string, ...any) {
		return logger.InfoContext
	}
	DefaultLogResponseLevel = func(logger flcore.Logger) func(context.Context, string, ...any) {
		return logger.InfoContext
	}
)

type PolicyLogRequest struct {
	Level func(flcore.Logger) func(context.Context, string, ...any)
}

func (p *PolicyLogRequest) Do(req *policy.Request) (*http.Response, error) {
	ctx := req.Raw().Context()
	logger, ok := flcore.TryFromContext[flcore.Logger](ctx)
	if !ok {
		return req.Next()
	}
	requestBytes, err := httputil.DumpRequest(req.Raw(), true)
	if err != nil {
		logger.ErrorContext(ctx, "failed to dump request", "error", err)
		return req.Next()
	}
	logLevel := DefaultLogRequestLevel
	if p.Level != nil {
		logLevel = p.Level
	}
	logLevel(logger)(ctx, "HTTP Request\n"+string(requestBytes))
	return req.Next()
}

type PolicyLogResponse struct {
	Level func(flcore.Logger) func(context.Context, string, ...any)
}

func (p *PolicyLogResponse) Do(req *policy.Request) (*http.Response, error) {
	ctx := req.Raw().Context()
	resp, err := req.Next()
	if err != nil {
		return resp, err
	}
	logger, ok := flcore.TryFromContext[flcore.Logger](ctx)
	if !ok {
		return resp, err
	}
	respBytes, err := httputil.DumpResponse(resp, true)
	if err != nil {
		logger.ErrorContext(ctx, "failed to dump response", "error", err)
		return resp, err
	}
	logLevel := DefaultLogResponseLevel
	if p.Level != nil {
		logLevel = p.Level
	}
	logLevel(logger)(ctx, "HTTP Response\n"+string(respBytes))
	return resp, err
}

type PolicySetQueryParameters map[string]string

func (p PolicySetQueryParameters) Do(req *policy.Request) (*http.Response, error) {
	q := req.Raw().URL.Query()
	for k, v := range p {
		q.Set(k, v)
	}
	req.Raw().URL.RawQuery = q.Encode()
	return req.Next()
}
