package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/dg-i/terraform-provider-rcodezero/internal/rcode0api"
)

// apiError is an error reported by the RcodeZero API.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.status, e.message)
}

func isNotFound(err error) bool {
	var e *apiError
	return errors.As(err, &e) && e.status == http.StatusNotFound
}

// parseResponse evaluates the result of a generated client call (it doesn't
// send anything itself). It checks the HTTP status before handing the response
// to a generated Parse*Result function. The API's error format is
// undocumented, and the generated parsers fail on error bodies that don't
// decode, which would hide a 404 or the real status.
//
// The API also reports some errors with the expected 2xx status and
// {"status":"failed"} in the body (seen for PATCH zone_disabled on a slave
// zone), so a 2xx body is checked as well.
func parseResponse[T any](httpResp *http.Response, err error, want int, parse func(*http.Response) (*T, error)) (*T, error) {
	if err != nil {
		return nil, err
	}
	body, err := io.ReadAll(httpResp.Body)
	_ = httpResp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if httpResp.StatusCode != want {
		return nil, &apiError{status: httpResp.StatusCode, message: errorMessage(body)}
	}
	var e rcode0api.Error
	if json.Unmarshal(body, &e) == nil && e.Status == rcode0api.Failed {
		return nil, &apiError{status: httpResp.StatusCode, message: `status "failed": ` + e.Message}
	}
	httpResp.Body = io.NopCloser(bytes.NewReader(body))
	res, err := parse(httpResp)
	if err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return res, nil
}

func errorMessage(body []byte) string {
	var e rcode0api.Error
	if json.Unmarshal(body, &e) == nil && e.Message != "" {
		return e.Message
	}
	if len(body) == 0 {
		return "(empty body)"
	}
	return string(body)
}
