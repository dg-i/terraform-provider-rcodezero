// Package mockserver is an in-memory fake of the RcodeZero zone API. Its handlers
// are generated from the same vendored spec as the client, and every request is
// validated against that spec, so the provider cannot drift from the documented
// API without the tests noticing.
package mockserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/getkin/kin-openapi/openapi3filter"
	nethttpmiddleware "github.com/oapi-codegen/nethttp-middleware"
)

var nsset = []string{"sec1.rcode0.net", "sec2.rcode0.net"}

// Server holds the zones of a single fake RcodeZero account.
type Server struct {
	mu     sync.Mutex
	zones  map[string]*ZoneDetails
	nextID float32
}

var _ StrictServerInterface = (*Server)(nil)

func New() *Server {
	return &Server{zones: map[string]*ZoneDetails{}, nextID: 1}
}

// Handler returns the HTTP handler that serves the API. Requests must carry
// "Authorization: Bearer <token>".
func (s *Server) Handler(token string) (http.Handler, error) {
	spec, err := GetSwagger()
	if err != nil {
		return nil, fmt.Errorf("loading embedded spec: %w", err)
	}
	spec.Servers = nil

	validate := nethttpmiddleware.OapiRequestValidatorWithOptions(spec, &nethttpmiddleware.Options{
		Options: openapi3filter.Options{
			AuthenticationFunc: func(_ context.Context, in *openapi3filter.AuthenticationInput) error {
				if in.RequestValidationInput.Request.Header.Get("Authorization") != "Bearer "+token {
					return errors.New("invalid or missing bearer token")
				}
				return nil
			},
		},
		ErrorHandlerWithOpts: func(_ context.Context, err error, w http.ResponseWriter, _ *http.Request, opts nethttpmiddleware.ErrorHandlerOpts) {
			writeError(w, opts.StatusCode, err.Error())
		},
	})

	mux := http.NewServeMux()
	HandlerFromMux(NewStrictHandler(s, nil), mux)
	return validate(mux), nil
}

// Zone returns a copy of the stored zone, for test assertions.
func (s *Server) Zone(domain string) (ZoneDetails, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	z, ok := s.zones[key(domain)]
	if !ok {
		return ZoneDetails{}, false
	}
	return *z, true
}

// RemoveZone deletes a zone behind the provider's back, to simulate drift.
func (s *Server) RemoveZone(domain string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.zones, key(domain))
}

func (s *Server) AddZone(_ context.Context, req AddZoneRequestObject) (AddZoneResponseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	b := req.Body
	if req.Params.Dnssec != nil && *req.Params.Dnssec == 1 {
		return AddZonedefaultJSONResponse{StatusCode: http.StatusNotImplemented, Body: errBody("DNSSEC is not supported by the mock")}, nil
	}
	if _, exists := s.zones[key(b.Domain)]; exists {
		return AddZonedefaultJSONResponse{StatusCode: http.StatusConflict, Body: errBody("Zone " + b.Domain + " already exists")}, nil
	}
	masters := deref(b.Masters)
	if msg := checkZone(strings.ToUpper(string(b.Type)), masters, b.SerialAutoIncrement); msg != "" {
		return AddZonedefaultJSONResponse{StatusCode: http.StatusUnprocessableEntity, Body: errBody(msg)}, nil
	}

	if req.Params.Test == nil || *req.Params.Test != 1 {
		serial := float32(time.Now().Unix())
		typ := strings.ToUpper(string(b.Type))
		s.zones[key(b.Domain)] = &ZoneDetails{
			Id:                  s.nextID,
			Domain:              key(b.Domain),
			Type:                TypeWithMember(typ),
			Masters:             masters,
			Serial:              &serial,
			Created:             time.Now().UTC().Format(time.RFC3339),
			DnssecStatus:        Unsigned,
			DnssecStatusDetail:  "Zone is not signed",
			Nsset:               nsset,
			ZoneDisabled:        false,
			CdsCdnskeyPublish:   b.CdsCdnskeyPublish != nil && *b.CdsCdnskeyPublish,
			SerialAutoIncrement: serialAutoIncrement(typ, b.SerialAutoIncrement, nil),
		}
		s.nextID++
	}
	return AddZone201JSONResponse{Status: Ok, Message: "Zone " + b.Domain + " successfully added", Nsset: nsset}, nil
}

func (s *Server) GetZoneDetails(_ context.Context, req GetZoneDetailsRequestObject) (GetZoneDetailsResponseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	z, ok := s.zones[key(req.Zone)]
	if !ok {
		return GetZoneDetailsdefaultJSONResponse{StatusCode: http.StatusNotFound, Body: notFound(req.Zone)}, nil
	}
	return GetZoneDetails200JSONResponse(*z), nil
}

func (s *Server) UpdateZone(_ context.Context, req UpdateZoneRequestObject) (UpdateZoneResponseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	z, ok := s.zones[key(req.Zone)]
	if !ok {
		return UpdateZonedefaultJSONResponse{StatusCode: http.StatusNotFound, Body: notFound(req.Zone)}, nil
	}
	b := req.Body
	typ, masters := string(z.Type), z.Masters
	if b.Type != nil {
		typ = string(*b.Type)
	}
	if b.Masters != nil {
		masters = *b.Masters
	}
	if msg := checkZone(typ, masters, b.SerialAutoIncrement); msg != "" {
		return UpdateZonedefaultJSONResponse{StatusCode: http.StatusUnprocessableEntity, Body: errBody(msg)}, nil
	}
	z.Type, z.Masters = TypeWithMember(typ), masters
	z.SerialAutoIncrement = serialAutoIncrement(typ, b.SerialAutoIncrement, z.SerialAutoIncrement)
	if b.CdsCdnskeyPublish != nil {
		z.CdsCdnskeyPublish = *b.CdsCdnskeyPublish
	}
	return UpdateZone200JSONResponse{Status: Ok, Message: "Zone " + req.Zone + " successfully updated"}, nil
}

func (s *Server) PatchZone(_ context.Context, req PatchZoneRequestObject) (PatchZoneResponseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	z, ok := s.zones[key(req.Zone)]
	if !ok {
		return PatchZonedefaultJSONResponse{StatusCode: http.StatusNotFound, Body: notFound(req.Zone)}, nil
	}
	if req.Body.ZoneDisabled != nil {
		// Like the real API: an error, but with HTTP 200.
		if z.Type != TypeWithMemberMASTER {
			return PatchZone200JSONResponse{Status: Failed, Message: "'zone_disabled' is only supported for primary domains"}, nil
		}
		z.ZoneDisabled = *req.Body.ZoneDisabled
	}
	return PatchZone200JSONResponse{Status: Ok, Message: "Zone " + req.Zone + " successfully updated"}, nil
}

func (s *Server) DeleteZone(_ context.Context, req DeleteZoneRequestObject) (DeleteZoneResponseObject, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.zones[key(req.Zone)]; !ok {
		return DeleteZonedefaultJSONResponse{StatusCode: http.StatusNotFound, Body: notFound(req.Zone)}, nil
	}
	delete(s.zones, key(req.Zone))
	return DeleteZone200JSONResponse{Status: Ok, Message: "Zone " + req.Zone + " successfully removed"}, nil
}

func checkZone(typ string, masters []string, serialAutoIncrement *bool) string {
	switch {
	case typ == string(TypeSLAVE) && len(masters) == 0:
		return "slave zones require at least one master"
	case typ == string(TypeMASTER) && len(masters) > 0:
		return "master zones must not have masters"
	// The spec documents serial_auto_increment as "only valid for primary zones".
	case typ != string(TypeMASTER) && serialAutoIncrement != nil:
		return "serial_auto_increment is only valid for master zones"
	}
	return ""
}

// serialAutoIncrement mirrors the real API: the field is absent for slave
// zones and defaults to true for master zones.
func serialAutoIncrement(typ string, requested, current *bool) *bool {
	switch {
	case typ != string(TypeMASTER):
		return nil
	case requested != nil:
		return requested
	case current != nil:
		return current
	}
	t := true
	return &t
}

func key(domain string) string {
	return strings.ToLower(strings.TrimSuffix(domain, "."))
}

func deref(p *[]string) []string {
	if p == nil {
		return []string{}
	}
	return *p
}

func errBody(msg string) Error { return Error{Status: Failed, Message: msg} }

func notFound(zone string) Error { return errBody("Zone " + zone + " not found") }

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errBody(msg))
}
