package server

import (
	"net/http"
	"strings"
)

// routeMux records the method-aware patterns registered during startup. The
// OpenAPI surface is generated from this list, so documentation cannot drift
// into routes that the server does not actually serve.
type routeMux struct {
	*http.ServeMux
	routes []routeRegistration
}

type routeRegistration struct {
	Method string
	Path   string
}

func newRouteMux() *routeMux {
	return &routeMux{ServeMux: http.NewServeMux()}
}

func (m *routeMux) Handle(pattern string, handler http.Handler) {
	if method, path, ok := strings.Cut(pattern, " "); ok && method != "" && strings.HasPrefix(path, "/") {
		m.routes = append(m.routes, routeRegistration{Method: method, Path: strings.TrimSuffix(path, "{$}")})
	}
	m.ServeMux.Handle(pattern, handler)
}

func (m *routeMux) HandleFunc(pattern string, handler http.HandlerFunc) {
	m.Handle(pattern, handler)
}
