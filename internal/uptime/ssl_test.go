package uptime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Each child uses the real system-root loader in a fresh process. Verification
// stays enabled; no client transport or trust-pool field is modified. The peer
// observes the actual handshake and connection close, including absence of HTTP.
func TestProbeSSLVerifiedCertificates(t *testing.T) {
	if os.Getenv("GOODSENTRY_SSL_PROBE_HELPER") == "1" {
		sslProbeChild(t)
		return
	}
	ca := sslAuthority(t)
	otherCA := sslAuthority(t)
	now := time.Now()
	valid := ca.leaf(t, "localhost", true, now.Add(-time.Hour), now.Add(time.Hour))
	expired := ca.leaf(t, "localhost", true, now.Add(-2*time.Hour), now.Add(-time.Hour))
	future := ca.leaf(t, "localhost", true, now.Add(time.Hour), now.Add(2*time.Hour))
	wrongName := ca.leaf(t, "other.invalid", false, now.Add(-time.Hour), now.Add(time.Hour))
	untrusted := otherCA.leaf(t, "localhost", true, now.Add(-time.Hour), now.Add(time.Hour))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, host, reasons string
		certificates        []tls.Certificate
		oldProtocol         bool
		blockPrivate        bool
	}{
		{name: "trusted DNS and private policy", host: "localhost", reasons: "ok", certificates: []tls.Certificate{valid}, blockPrivate: true},
		{name: "trusted IP", host: "127.0.0.1", reasons: "ok", certificates: []tls.Certificate{valid}},
		{name: "hostname", host: "localhost", reasons: "tls_error", certificates: []tls.Certificate{wrongName}},
		{name: "untrusted root", host: "localhost", reasons: "tls_error", certificates: []tls.Certificate{untrusted}},
		{name: "expired", host: "localhost", reasons: "tls_error", certificates: []tls.Certificate{expired}},
		{name: "future", host: "localhost", reasons: "tls_error", certificates: []tls.Certificate{future}},
		{name: "certificate rotation", host: "localhost", reasons: "ok,tls_error", certificates: []tls.Certificate{valid, expired}},
		{name: "protocol below TLS 1.2", host: "localhost", reasons: "tls_error", certificates: []tls.Certificate{valid}, oldProtocol: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			reasons := strings.Split(test.reasons, ",")
			var certificateRequests atomic.Int64
			config := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
				index := min(int(certificateRequests.Add(1)-1), len(test.certificates)-1)
				return &test.certificates[index], nil
			}}
			if len(test.certificates) > 1 {
				// TLS1.2 supplies its session ticket during the handshake. A
				// wrongly enabled client cache could otherwise hide rotation.
				config.MaxVersion = tls.VersionTLS12
			}
			if test.oldProtocol {
				config.MinVersion, config.MaxVersion = tls.VersionTLS10, tls.VersionTLS11
			}
			address, observations := sslPeer(t, len(reasons), func(raw net.Conn, _ int) sslObservation {
				peer := tls.Server(raw, config)
				observation := sslObservation{handshakeError: peer.Handshake()}
				if observation.handshakeError == nil {
					state := peer.ConnectionState()
					observation.serverName, observation.version, observation.resumed = state.ServerName, state.Version, state.DidResume
					var data [1]byte
					observation.bytes, observation.closeError = peer.Read(data[:])
				}
				return observation
			})
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestProbeSSLVerifiedCertificates$", "-test.timeout=8s")
			command.Env = append(os.Environ(), "GOODSENTRY_SSL_PROBE_HELPER=1", "GOODSENTRY_SSL_PROBE_URL=https://"+net.JoinHostPort(test.host, port), "GOODSENTRY_SSL_PROBE_REASONS="+test.reasons, "SSL_CERT_FILE="+ca.path, "SSL_CERT_DIR="+t.TempDir())
			if test.blockPrivate {
				command.Env = append(command.Env, "GOODSENTRY_SSL_PROBE_BLOCK_PRIVATE=1")
			}
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("SSL probe child failed: %v\n%s", err, output)
			}
			for _, reason := range reasons {
				observation := sslObserve(t, observations)
				if observation.bytes != 0 {
					t.Fatal("SSL check sent application/HTTP bytes", observation.bytes)
				}
				if reason == "ok" {
					wantSNI := test.host
					if net.ParseIP(test.host) != nil {
						wantSNI = ""
					}
					if observation.handshakeError != nil || observation.serverName != wantSNI || observation.version < tls.VersionTLS12 || observation.resumed || !sslEOF(observation.closeError) {
						t.Fatalf("verified TLS/close/SNI observation: %+v", observation)
					}
				} else if observation.handshakeError == nil {
					t.Fatal("invalid certificate/protocol completed a server handshake")
				}
			}
		})
	}
}

func sslProbeChild(t *testing.T) {
	t.Helper()
	target := os.Getenv("GOODSENTRY_SSL_PROBE_URL")
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "https" || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") || u.Port() == "" {
		t.Fatal("SSL helper requires the parent localhost fixture")
	}
	c := probeConfig(Kind("SSL"), target)
	c.ExpectedStatus = 599 // Required configuration round trip, unused by SSL.
	if os.Getenv("GOODSENTRY_SSL_PROBE_BLOCK_PRIVATE") == "1" {
		blocked := NewProber(false)
		result, err := blocked.Check(context.Background(), c)
		blocked.Close()
		if err != nil || result.Up || result.Reason != "destination_blocked" || result.StatusCode != nil || result.ResponseTimeMS == nil {
			t.Fatalf("SSL private-target admission: %+v %v", result, err)
		}
	}
	p := NewProber(true)
	defer p.Close()
	for _, reason := range strings.Split(os.Getenv("GOODSENTRY_SSL_PROBE_REASONS"), ",") {
		if reason != "ok" && reason != "tls_error" {
			t.Fatal("invalid parent SSL expectation")
		}
		result, err := p.Check(context.Background(), c)
		if err != nil || result.Up != (reason == "ok") || result.Reason != reason || result.StatusCode != nil || result.ResponseTimeMS == nil || *result.ResponseTimeMS < 0 {
			t.Fatalf("SSL certificate check expected %s: %+v %v", reason, result, err)
		}
	}
}

func TestProbeSSLHandshakeTimeoutAndCancellation(t *testing.T) {
	started := make(chan struct{}, 2)
	address, observations := sslPeer(t, 2, func(raw net.Conn, _ int) sslObservation {
		var data [1]byte
		if _, err := io.ReadFull(raw, data[:]); err != nil {
			return sslObservation{handshakeError: err}
		}
		started <- struct{}{}
		_, err := io.Copy(io.Discard, raw) // Read ClientHello, but never reply.
		return sslObservation{closeError: err}
	})
	p := NewProber(true)
	defer p.Close()
	c := probeConfig(Kind("SSL"), "https://"+address)
	c.TimeoutSeconds = 1
	type outcome struct {
		result Result
		err    error
	}
	check := func(ctx context.Context) <-chan outcome {
		done := make(chan outcome, 1)
		go func() { result, err := p.Check(ctx, c); done <- outcome{result, err} }()
		return done
	}
	await := func(done <-chan outcome) outcome {
		t.Helper()
		select {
		case result := <-done:
			return result
		case <-time.After(3 * time.Second):
			t.Fatal("SSL handshake did not stop within configured/caller deadline")
			return outcome{}
		}
	}
	awaitStart := func() {
		t.Helper()
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("probe never reached the actual TLS handshake")
		}
	}
	done := check(context.Background())
	awaitStart()
	result := await(done)
	if result.err != nil || result.result.Up || result.result.Reason != "timeout" || result.result.StatusCode != nil || result.result.ResponseTimeMS == nil || *result.result.ResponseTimeMS <= 0 {
		t.Fatalf("SSL handshake timeout: %+v %v", result.result, result.err)
	}
	if observation := sslObserve(t, observations); observation.handshakeError != nil || observation.closeError != nil {
		t.Fatalf("timed-out SSL connection remained open: %+v", observation)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done = check(ctx)
	awaitStart()
	cancel()
	result = await(done)
	if !errors.Is(result.err, context.Canceled) || result.result != (Result{}) {
		t.Fatalf("caller cancellation became an SSL outage: %+v %v", result.result, result.err)
	}
	if observation := sslObserve(t, observations); observation.handshakeError != nil || observation.closeError != nil {
		t.Fatalf("canceled SSL connection remained open: %+v", observation)
	}
}

type sslObservation struct {
	handshakeError, closeError error
	serverName                 string
	version                    uint16
	resumed                    bool
	bytes                      int
}

// A finite peer uses real sockets and observes client closure. Cleanup closes
// both listener and current connection, including when a check fails early.
func sslPeer(t *testing.T, connections int, handle func(net.Conn, int) sslObservation) (string, <-chan sslObservation) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	observations := make(chan sslObservation, connections)
	done := make(chan struct{})
	var mu sync.Mutex
	var active net.Conn
	var closing bool
	go func() {
		defer close(done)
		defer close(observations)
		for index := range connections {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			if closing {
				mu.Unlock()
				_ = raw.Close()
				return
			}
			active = raw
			mu.Unlock()
			_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
			observation := handle(raw, index)
			_ = raw.Close()
			mu.Lock()
			active = nil
			mu.Unlock()
			observations <- observation
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		mu.Lock()
		closing = true
		if active != nil {
			_ = active.Close()
		}
		mu.Unlock()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("SSL fixture did not stop")
		}
	})
	return listener.Addr().String(), observations
}

func sslObserve(t *testing.T, observations <-chan sslObservation) sslObservation {
	t.Helper()
	select {
	case observation, ok := <-observations:
		if !ok {
			t.Fatal("probe reported an outcome without a peer connection")
		}
		return observation
	case <-time.After(3 * time.Second):
		t.Fatal("SSL probe did not close its peer connection")
		return sslObservation{}
	}
}

func sslEOF(err error) bool { return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) }

type sslTestAuthority struct {
	certificate *x509.Certificate
	key         *ecdsa.PrivateKey
	der         []byte
	path        string
}

func sslAuthority(t *testing.T) sslTestAuthority {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "GoodSentry SSL probe test CA"}, NotBefore: time.Now().Add(-24 * time.Hour), NotAfter: time.Now().Add(24 * time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return sslTestAuthority{ca, key, der, path}
}

func (ca sslTestAuthority) leaf(t *testing.T, name string, ip bool, validFrom, validUntil time.Time) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name}, NotBefore: validFrom, NotAfter: validUntil, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if ip {
		leaf.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca.certificate, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.der}, PrivateKey: key}
}
