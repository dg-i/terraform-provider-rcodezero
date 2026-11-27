package provider

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-log/tflogtest"
)

// Given an authenticated API call with trace logging on, when it goes through
// the logging transport, then request and response are logged without the
// token, and the caller still gets the full response body.
func TestLoggingTransport(t *testing.T) {
	// given
	const token = "secret-token-4711"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok","message":"Zone example.at successfully added"}`)
	}))
	t.Cleanup(srv.Close)

	var logs bytes.Buffer
	ctx := tflogtest.RootLogger(t.Context(), &logs)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v2/zones",
		strings.NewReader(`{"domain":"example.at","type":"master"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	// when
	resp, err := (&http.Client{Transport: loggingTransport{next: http.DefaultTransport}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	// then the body survives being buffered for logging
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "successfully added") {
		t.Errorf("response body lost after logging: %q", body)
	}

	// and the logs contain the call but never the token
	out := logs.String()
	if strings.Contains(out, token) {
		t.Fatalf("token leaked into the logs:\n%s", out)
	}
	for _, want := range []string{
		`"@message":"RcodeZero API request"`, // DEBUG: method, URL, status
		`"status":200`,                       //
		`"body":"{\"domain\":\"example.at\"`, // TRACE: request body
		`successfully added`,                 // TRACE: response body
	} {
		if !strings.Contains(out, want) {
			t.Errorf("logs don't contain %s:\n%s", want, out)
		}
	}
}
