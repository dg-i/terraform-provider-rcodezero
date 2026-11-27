package acceptance

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/dg-i/terraform-provider-rcodezero/internal/mockserver"
	rcodezero "github.com/dg-i/terraform-provider-rcodezero/internal/provider"
)

const testToken = "test-token"

var testProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"rcodezero": providerserver.NewProtocol6WithError(rcodezero.New("test")()),
}

// TestMain points terraform-plugin-testing at OpenTofu instead of letting it
// download Terraform.
func TestMain(m *testing.M) {
	if tofu, err := exec.LookPath("tofu"); err == nil {
		setenvDefault("TF_ACC_TERRAFORM_PATH", tofu)
	}
	setenvDefault("TF_ACC_PROVIDER_HOST", "registry.opentofu.org")
	os.Exit(m.Run())
}

func setenvDefault(key, value string) {
	if os.Getenv(key) != "" {
		return
	}
	if err := os.Setenv(key, value); err != nil {
		log.Fatal(err)
	}
}

// startMock runs an in-memory RcodeZero API for the duration of the test and
// returns it with a provider block pointing at it, plus a hook to make it
// misbehave.
func startMock(t *testing.T) (*mockserver.Server, string, *faults) {
	t.Helper()
	mock := mockserver.New()
	h, err := mock.Handler(testToken)
	if err != nil {
		t.Fatal(err)
	}
	f := &faults{}
	srv := httptest.NewServer(f.wrap(h))
	t.Cleanup(srv.Close)

	return mock, `
provider "rcodezero" {
  endpoint  = "` + srv.URL + `"
  api_token = "` + testToken + `"
}
`, f
}

// faults lets a test answer selected requests itself, to simulate API
// behaviour the spec doesn't document. A fault returns true if it answered.
type faults struct {
	mu sync.Mutex
	fn func(http.ResponseWriter, *http.Request) bool
}

func (f *faults) set(fn func(http.ResponseWriter, *http.Request) bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fn = fn
}

func (f *faults) clear() { f.set(nil) }

func (f *faults) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		fn := f.fn
		f.mu.Unlock()
		if fn != nil && fn(w, r) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// respondOn answers requests with the given method with a fixed response.
func respondOn(method string, status int, contentType, body string) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if r.Method != method {
			return false
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
		return true
	}
}
