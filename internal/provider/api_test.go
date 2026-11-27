package provider

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dg-i/terraform-provider-rcodezero/internal/rcode0api"
)

const jsonType = "application/json"

// apiResponse is what a fake RcodeZero API answers in a test.
type apiResponse struct {
	status      int
	contentType string
	body        string
}

func (r apiResponse) httpResponse() *http.Response {
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", r.contentType)
	rec.WriteHeader(r.status)
	_, _ = rec.WriteString(r.body)
	return rec.Result()
}

// Given an API response, when parseResponse evaluates it, then it returns the
// parsed result or an error that says what went wrong.
func TestParseResponse(t *testing.T) {
	type expected struct {
		errContains string // empty means success
		notFound    bool
		json200     bool
	}

	tests := []struct {
		name     string
		given    apiResponse
		givenErr error // the request itself failed
		then     expected
	}{
		{
			name:  "zone",
			given: apiResponse{200, jsonType, slaveZoneJSON},
			then:  expected{json200: true},
		},
		{
			name:  "not found, as returned by the real API",
			given: apiResponse{404, jsonType, `{"status":"failed","message":"Zone not found"}`},
			then:  expected{errContains: "HTTP 404: Zone not found", notFound: true},
		},
		{
			name:  "not found with empty body",
			given: apiResponse{404, jsonType, ""},
			then:  expected{errContains: "HTTP 404: (empty body)", notFound: true},
		},
		{
			name:  "error with HTTP 200, as returned by the real API",
			given: apiResponse{200, jsonType, `{"status":"failed","message":"'zone_disabled' is only supported for primary domains"}`},
			then:  expected{errContains: `HTTP 200: status "failed": 'zone_disabled' is only supported`},
		},
		{
			name:  "unauthorized as JSON string, as returned by the real API",
			given: apiResponse{401, jsonType, `"Unauthorized"`},
			then:  expected{errContains: `HTTP 401: "Unauthorized"`},
		},
		{
			// read() turns the missing JSON200 into an error; parseResponse itself accepts it.
			name:  "HTML with HTTP 200",
			given: apiResponse{200, "text/html", "<html>login</html>"},
			then:  expected{},
		},
		{
			name:     "request failed",
			givenErr: errors.New("connection refused"),
			then:     expected{errContains: "connection refused"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			var httpResp *http.Response
			if tt.givenErr == nil {
				httpResp = tt.given.httpResponse()
			}

			// when
			res, err := parseResponse(httpResp, tt.givenErr, http.StatusOK, rcode0api.ParseGetZoneDetailsResult)

			// then
			switch {
			case tt.then.errContains == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.then.errContains != "" && err == nil:
				t.Fatalf("expected error containing %q, got none", tt.then.errContains)
			case tt.then.errContains != "" && !strings.Contains(err.Error(), tt.then.errContains):
				t.Fatalf("error %q doesn't contain %q", err, tt.then.errContains)
			}
			if isNotFound(err) != tt.then.notFound {
				t.Errorf("isNotFound = %v, want %v", isNotFound(err), tt.then.notFound)
			}
			if err == nil && (res.JSON200 != nil) != tt.then.json200 {
				t.Errorf("JSON200 set = %v, want %v", res.JSON200 != nil, tt.then.json200)
			}
		})
	}
}
