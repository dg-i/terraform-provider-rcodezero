package provider

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// loggingTransport logs every API call via tflog: method, URL, status and
// duration at DEBUG, request and response bodies at TRACE. Headers are never
// logged, so the API token stays out of the logs.
type loggingTransport struct {
	next http.RoundTripper
}

func (t loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	fields := map[string]any{"method": req.Method, "url": req.URL.String()}

	if req.GetBody != nil {
		if body, err := req.GetBody(); err == nil {
			b, _ := io.ReadAll(body)
			tflog.Trace(ctx, "RcodeZero API request body", map[string]any{"method": req.Method, "url": req.URL.String(), "body": string(b)})
		}
	}

	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	fields["duration_ms"] = time.Since(start).Milliseconds()
	if err != nil {
		fields["error"] = err.Error()
		tflog.Debug(ctx, "RcodeZero API request failed", fields)
		return nil, err
	}
	fields["status"] = resp.StatusCode
	tflog.Debug(ctx, "RcodeZero API request", fields)

	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	resp.Body = io.NopCloser(bytes.NewReader(b))
	fields["body"] = string(b)
	tflog.Trace(ctx, "RcodeZero API response body", fields)
	return resp, nil
}
