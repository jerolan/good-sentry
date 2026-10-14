package uptime

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"goodsentry/internal/nettarget"
)

const (
	ReasonOK                 = "ok"
	ReasonInvalidConfig      = "invalid_config"
	ReasonHeartbeatNotProbed = "heartbeat_not_probed"
	ReasonDestinationBlocked = "destination_blocked"
	ReasonDNS                = "dns_error"
	ReasonConnection         = "connection_error"
	ReasonTLS                = "tls_error"
	ReasonTimeout            = "timeout"
	ReasonRedirectLimit      = "redirect_limit"
	ReasonHTTP               = "http_error"
	ReasonUnexpectedStatus   = "unexpected_status"
	ReasonBodyMismatch       = "body_mismatch"
	maxBodyBytes             = 1 << 20
	maxHeaderBytes           = 64 << 10
)

var (
	errRedirectLimit  = errors.New(ReasonRedirectLimit)
	errRedirectTarget = errors.New(ReasonInvalidConfig)
)

// Validate normalizes public configuration without resolving or contacting targets.
func (c *Config) Validate() error {
	if c == nil {
		return errors.New("monitor configuration is required")
	}
	c.Name, c.URL, c.Project = strings.TrimSpace(c.Name), strings.TrimSpace(c.URL), strings.TrimSpace(c.Project)
	for _, value := range []string{c.Name, c.URL, c.Project, c.Environment, c.ExpectedBody, string(c.Type)} {
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return errors.New("monitor text must be valid Unicode without NUL")
		}
	}
	if utf8.RuneCountInString(c.Environment) > 128 {
		return errors.New("environment must contain at most 128 characters")
	}
	if n := utf8.RuneCountInString(c.Name); n < 1 || n > 200 {
		return errors.New("name must contain 1 to 200 characters")
	}
	if utf8.RuneCountInString(c.URL) > 2000 || utf8.RuneCountInString(c.ExpectedBody) > 2000 {
		return errors.New("URL and expected body must contain at most 2000 characters")
	}
	if c.IntervalSeconds < 1 || c.IntervalSeconds > 86400 {
		return errors.New("intervalSeconds must be between 1 and 86400")
	}
	if c.TimeoutSeconds < 1 || c.TimeoutSeconds > 60 {
		return errors.New("timeoutSeconds must be between 1 and 60")
	}
	if c.ConfirmationThreshold < 1 || c.ConfirmationThreshold > 100 {
		return errors.New("confirmationThreshold must be between 1 and 100")
	}
	if c.ExpectedStatus < 100 || c.ExpectedStatus > 599 {
		return errors.New("expectedStatus must be between 100 and 599")
	}
	if c.ExpectedBody != "" && c.Type != GET && c.Type != POST {
		return errors.New("expectedBody is supported only for GET and POST")
	}
	switch c.Type {
	case Heartbeat:
		if c.URL != "" {
			return errors.New("heartbeat URL must be empty")
		}
	case TCP:
		host, port, err := net.SplitHostPort(c.URL)
		if err != nil || !validHost(host) || !validPort(port) || (strings.HasPrefix(c.URL, "[") && !strings.Contains(host, ":")) {
			return errors.New("TCP target must be host:port with a numeric port between 1 and 65535")
		}
	case HEAD, GET, POST:
		u, err := url.Parse(c.URL)
		if err != nil || strings.Contains(c.URL, "#") || !validHTTPURL(u) {
			return errors.New("URL must be absolute HTTP or HTTPS without credentials or fragments")
		}
	default:
		return errors.New("unsupported monitor type")
	}
	return nil
}

func validPort(port string) bool {
	if port == "" {
		return false
	}
	for _, ch := range port {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}

func validHost(host string) bool {
	if host == "" || strings.ContainsAny(host, "\\/?#@") || strings.IndexFunc(host, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return false
	}
	if strings.Contains(host, ":") {
		_, err := netip.ParseAddr(host)
		return err == nil
	}
	return true
}

func validHTTPURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Opaque != "" || u.User != nil || u.Fragment != "" || !validHost(u.Hostname()) {
		return false
	}
	if strings.HasPrefix(u.Host, "[") {
		if _, err := netip.ParseAddr(u.Hostname()); err != nil {
			return false
		}
	}
	return (!strings.HasSuffix(u.Host, ":") && u.Port() == "") || validPort(u.Port())
}

// Prober can be shared by scheduler workers. The transport never uses proxy
// environment variables; each new connection resolves and pins its destination.
type Prober struct {
	dialer    nettarget.Dialer
	transport *http.Transport
	client    *http.Client
}

func NewProber(allowPrivate bool) *Prober {
	p := &Prober{dialer: nettarget.Dialer{AllowPrivate: allowPrivate}}
	p.transport = &http.Transport{
		DialContext: p.dialer.DialContext, MaxResponseHeaderBytes: maxHeaderBytes,
		MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second,
	}
	p.client = &http.Client{Transport: p.transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 5 {
			return errRedirectLimit
		}
		if !validHTTPURL(req.URL) || (req.Response != nil && strings.Contains(req.Response.Header.Get("Location"), "#")) {
			return errRedirectTarget
		}
		return nil
	}}
	return p
}

func (p *Prober) Close() { p.transport.CloseIdleConnections() }

// Check returns caller cancellation as an error so shutdown cannot become an
// outage. Probe deadlines and other failures are bounded, non-sensitive results.
func (p *Prober) Check(ctx context.Context, c Config) (result Result, err error) {
	defer func() {
		if ctx.Err() != nil {
			result, err = Result{}, ctx.Err()
		}
	}()
	if c.Validate() != nil {
		return Result{Reason: ReasonInvalidConfig}, nil
	}
	if c.Type == Heartbeat {
		return Result{Reason: ReasonHeartbeatNotProbed}, nil
	}
	started := time.Now()
	defer func() { elapsed := time.Since(started).Milliseconds(); result.ResponseTimeMS = &elapsed }()
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(c.TimeoutSeconds)*time.Second)
	defer cancel()
	if c.Type == TCP {
		conn, dialErr := p.dialer.DialContext(probeCtx, "tcp", c.URL)
		if dialErr != nil {
			return Result{Reason: failureReason(dialErr, probeCtx, ReasonConnection)}, nil
		}
		conn.Close()
		return Result{Up: true, Reason: ReasonOK}, nil
	}
	req, requestErr := http.NewRequestWithContext(probeCtx, string(c.Type), c.URL, nil)
	if requestErr != nil {
		return Result{Reason: ReasonInvalidConfig}, nil
	}
	response, requestErr := p.client.Do(req)
	if requestErr != nil {
		return Result{Reason: failureReason(requestErr, probeCtx, ReasonHTTP)}, nil
	}
	defer response.Body.Close()
	result.StatusCode = &response.StatusCode
	if response.StatusCode != c.ExpectedStatus {
		result.Reason = ReasonUnexpectedStatus
		return result, nil
	}
	if c.ExpectedBody != "" {
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxBodyBytes+1))
		if readErr != nil {
			result.Reason = failureReason(readErr, probeCtx, ReasonHTTP)
			return result, nil
		}
		if len(body) > maxBodyBytes {
			body = body[:maxBodyBytes]
		}
		if !strings.Contains(string(body), c.ExpectedBody) {
			result.Reason = ReasonBodyMismatch
			return result, nil
		}
	}
	result.Up, result.Reason = true, ReasonOK
	return result, nil
}

func failureReason(err error, ctx context.Context, fallback string) string {
	var networkErr net.Error
	var certificateErr *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		return ReasonTimeout
	case errors.Is(err, nettarget.ErrBlocked):
		return ReasonDestinationBlocked
	case errors.Is(err, nettarget.ErrDNS):
		return ReasonDNS
	case errors.Is(err, errRedirectLimit):
		return ReasonRedirectLimit
	case errors.Is(err, errRedirectTarget):
		return ReasonInvalidConfig
	case errors.As(err, &certificateErr), errors.As(err, &unknownAuthority):
		return ReasonTLS
	case errors.As(err, &networkErr):
		if networkErr.Timeout() {
			return ReasonTimeout
		}
		var opErr *net.OpError
		if errors.As(err, &opErr) && opErr.Op == "dial" {
			return ReasonConnection
		}
	}
	return fallback
}
