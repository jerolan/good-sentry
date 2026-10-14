package uptime

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigValidate(t *testing.T) {
	c := Defaults()
	c.Name, c.URL, c.Project = "  estado 🙂  ", " https://example.invalid/health ", " web "
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Name != "estado 🙂" || c.URL != "https://example.invalid/health" || c.Project != "web" {
		t.Fatalf("configuration was not normalized: %#v", c)
	}
	valid := []Config{c, {Name: strings.Repeat("🙂", 200), Type: TCP, URL: "[::1]:5432", IntervalSeconds: 86400, TimeoutSeconds: 60, ExpectedStatus: 599, ConfirmationThreshold: 100}, {Name: "heartbeat", Type: Heartbeat, IntervalSeconds: 1, TimeoutSeconds: 1, ExpectedStatus: 100, ConfirmationThreshold: 1}}
	for _, config := range valid {
		if err := config.Validate(); err != nil {
			t.Errorf("valid configuration rejected: %v", err)
		}
	}
	cases := []struct {
		name string
		edit func(*Config)
	}{
		{"empty name", func(c *Config) { c.Name = " \n " }},
		{"long unicode name", func(c *Config) { c.Name = strings.Repeat("🙂", 201) }},
		{"invalid utf8", func(c *Config) { c.Name = string([]byte{255}) }},
		{"name nul", func(c *Config) { c.Name = "a\x00b" }},
		{"project nul", func(c *Config) { c.Project = "a\x00b" }},
		{"body nul", func(c *Config) { c.ExpectedBody = "\x00" }},
		{"url nul", func(c *Config) { c.URL = "https://example.invalid/\x00" }},
		{"kind", func(c *Config) { c.Type = "get" }},
		{"interval zero", func(c *Config) { c.IntervalSeconds = 0 }},
		{"interval large", func(c *Config) { c.IntervalSeconds = 86401 }},
		{"timeout zero", func(c *Config) { c.TimeoutSeconds = 0 }},
		{"timeout large", func(c *Config) { c.TimeoutSeconds = 61 }},
		{"threshold zero", func(c *Config) { c.ConfirmationThreshold = 0 }},
		{"threshold large", func(c *Config) { c.ConfirmationThreshold = 101 }},
		{"status small", func(c *Config) { c.ExpectedStatus = 99 }},
		{"status large", func(c *Config) { c.ExpectedStatus = 600 }},
		{"long url", func(c *Config) { c.URL = "https://example.invalid/" + strings.Repeat("a", 2000) }},
		{"relative url", func(c *Config) { c.URL = "/health" }},
		{"scheme", func(c *Config) { c.URL = "ftp://example.invalid/" }},
		{"credentials", func(c *Config) { c.URL = "https://user:pass@example.invalid/" }},
		{"fragment", func(c *Config) { c.URL = "https://example.invalid/#health" }},
		{"missing host", func(c *Config) { c.URL = "http:///health" }},
		{"invalid port", func(c *Config) { c.URL = "http://example.invalid:65536/" }},
		{"head body", func(c *Config) { c.ExpectedBody = "ready" }},
		{"long body", func(c *Config) { c.Type = GET; c.ExpectedBody = strings.Repeat("🙂", 2001) }},
		{"heartbeat url", func(c *Config) { c.Type = Heartbeat }},
		{"heartbeat body", func(c *Config) { c.Type = Heartbeat; c.URL = ""; c.ExpectedBody = "ready" }},
		{"tcp body", func(c *Config) { c.Type = TCP; c.URL = "localhost:80"; c.ExpectedBody = "ready" }},
		{"tcp missing port", func(c *Config) { c.Type = TCP; c.URL = "localhost" }},
		{"tcp named port", func(c *Config) { c.Type = TCP; c.URL = "localhost:http" }},
		{"tcp zero port", func(c *Config) { c.Type = TCP; c.URL = "localhost:0" }},
		{"tcp ipv6 brackets", func(c *Config) { c.Type = TCP; c.URL = "::1:80" }},
		{"tcp control character", func(c *Config) { c.Type = TCP; c.URL = "local\x01host:80" }},
		{"tcp bracketed hostname", func(c *Config) { c.Type = TCP; c.URL = "[localhost]:80" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := c
			tc.edit(&config)
			if err := config.Validate(); err == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestProbeHTTPMethodsAndStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != r.URL.Query().Get("method") || len(body) != 0 {
			t.Errorf("request method/body: %s %q", r.Method, body)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	p := NewProber(true)
	defer p.Close()
	for _, kind := range []Kind{HEAD, GET, POST} {
		c := probeConfig(kind, server.URL+"?method="+string(kind))
		c.ExpectedStatus = http.StatusCreated
		result, err := p.Check(context.Background(), c)
		if err != nil || !result.Up || result.StatusCode == nil || *result.StatusCode != 201 || result.ResponseTimeMS == nil || *result.ResponseTimeMS < 0 {
			t.Fatalf("%s: %#v %v", kind, result, err)
		}
		c.ExpectedStatus = 200
		result, err = p.Check(context.Background(), c)
		if err != nil || result.Up || result.Reason != "unexpected_status" {
			t.Fatalf("unexpected status: %#v %v", result, err)
		}
	}
}

func TestProbeBodyPrefixBound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/present":
			io.WriteString(w, "prefix ready suffix"+strings.Repeat("x", 1<<20))
		case "/boundary":
			io.WriteString(w, strings.Repeat("x", (1<<20)-5)+"ready")
		case "/stream":
			io.WriteString(w, "ready"+strings.Repeat("x", 1<<20))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		case "/past":
			io.WriteString(w, strings.Repeat("x", (1<<20)-4)+"ready")
		default:
			io.WriteString(w, "not available")
		}
	}))
	defer server.Close()
	p := NewProber(true)
	defer p.Close()
	for _, tc := range []struct {
		path string
		up   bool
	}{{"/present", true}, {"/boundary", true}, {"/stream", true}, {"/past", false}, {"/absent", false}} {
		c := probeConfig(GET, server.URL+tc.path)
		c.ExpectedBody = "ready"
		c.TimeoutSeconds = 1
		result, err := p.Check(context.Background(), c)
		if err != nil || result.Up != tc.up || (!tc.up && result.Reason != "body_mismatch") {
			t.Errorf("%s: %#v %v", tc.path, result, err)
		}
	}
}

func TestProbePrivateDestinationPolicy(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer server.Close()
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	p := NewProber(false)
	defer p.Close()
	for _, host := range []string{"127.0.0.1", "localhost", "[::ffff:127.0.0.1]", "0.0.0.0", "10.1.2.3", "169.254.1.1", "224.0.0.1", "[::1]", "[fe80::1]", "[ff02::1]", "[::]"} {
		result, err := p.Check(context.Background(), probeConfig(GET, "http://"+host+":"+port))
		if err != nil || result.Up || result.Reason != "destination_blocked" {
			t.Errorf("%s: %#v %v", host, result, err)
		}
		result, err = p.Check(context.Background(), probeConfig(TCP, host+":"+port))
		if err != nil || result.Up || result.Reason != "destination_blocked" {
			t.Errorf("TCP %s: %#v %v", host, result, err)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("blocked target received request")
	}
}

func TestProbeRedirects(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/credentials" {
			http.Redirect(w, r, "http://user:pass@"+r.Host+"/done", 302)
			return
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/"))
		if n > 0 {
			http.Redirect(w, r, "/"+strconv.Itoa(n-1), 302)
		}
	}))
	defer server.Close()
	p := NewProber(true)
	defer p.Close()
	for _, tc := range []struct {
		path, reason string
		up           bool
	}{{"/5", "ok", true}, {"/6", "redirect_limit", false}, {"/credentials", "invalid_config", false}} {
		calls.Store(0)
		result, err := p.Check(context.Background(), probeConfig(GET, server.URL+tc.path))
		if err != nil || result.Up != tc.up || result.Reason != tc.reason {
			t.Errorf("%s: %#v %v", tc.path, result, err)
		}
		if calls.Load() > 6 {
			t.Fatalf("followed too many redirects: %d", calls.Load())
		}
	}
}

func TestProbeTCPAndTLS(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	accepted := make(chan struct{})
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			conn.Close()
		}
		close(accepted)
	}()
	p := NewProber(true)
	defer p.Close()
	c := probeConfig(TCP, listener.Addr().String())
	result, err := p.Check(context.Background(), c)
	if err != nil || !result.Up || result.StatusCode != nil || result.ResponseTimeMS == nil {
		t.Fatalf("TCP up: %#v %v", result, err)
	}
	<-accepted
	listener.Close()
	result, err = p.Check(context.Background(), c)
	if err != nil || result.Up || result.Reason != "connection_error" {
		t.Fatalf("TCP down: %#v %v", result, err)
	}
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsServer.Close()
	result, err = p.Check(context.Background(), probeConfig(GET, tlsServer.URL))
	if err != nil || result.Up || result.Reason != "tls_error" {
		t.Fatalf("untrusted TLS accepted: %#v %v", result, err)
	}
}

func TestProbeCancellationAndTimeout(t *testing.T) {
	started := make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-r.Context().Done() }))
	defer server.Close()
	p := NewProber(true)
	defer p.Close()
	c := probeConfig(GET, server.URL)
	c.TimeoutSeconds = 1
	result, err := p.Check(context.Background(), c)
	if err != nil || result.Up || result.Reason != "timeout" {
		t.Fatalf("probe timeout: %#v %v", result, err)
	}
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := p.Check(ctx, c); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("caller cancellation recorded as result: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation did not stop probe")
	}
}

func TestProbeResponseHeadersBound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Overflow", strings.Repeat("x", 128<<10))
	}))
	defer server.Close()
	p := NewProber(true)
	defer p.Close()
	result, err := p.Check(context.Background(), probeConfig(GET, server.URL))
	if err != nil || result.Up || result.Reason != "http_error" {
		t.Fatalf("unbounded headers: %#v %v", result, err)
	}
}

func probeConfig(kind Kind, target string) Config {
	c := Defaults()
	c.Name, c.Type, c.URL = "probe", kind, target
	return c
}
