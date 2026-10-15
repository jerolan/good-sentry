package notify

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// SSL_CERT_FILE is an operator-facing trust setting documented by
// crypto/x509.SystemCertPool. A fresh process avoids its process-wide root cache.
func TestSMTPAuthenticatedTLS(t *testing.T) {
	if os.Getenv("GOODSENTRY_SMTP_TLS_HELPER") == "1" {
		raw := os.Getenv("GOODSENTRY_SMTP_TLS_URL")
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() != "127.0.0.1" || u.Port() == "" || (u.Scheme != "smtps" && u.Scheme != "smtp+starttls") {
			t.Fatal("TLS helper requires the parent localhost fixture")
		}
		opts := senderOptions()
		opts.EmailURL, opts.Timeout = raw, 3*time.Second
		sender, err := NewSender(opts)
		if err != nil {
			t.Fatal(err)
		}
		defer sender.Close()
		for i := 0; i < 2; i++ {
			outcome, err := sender.Deliver(context.Background(), delivery(Recipient{Type: "email", Destination: "admin@example.com"}))
			if err != nil || outcome.Disposition != Delivered || outcome.Reason != "accepted" || outcome.StatusCode == nil || *outcome.StatusCode != 250 {
				t.Fatalf("authenticated TLS SMTP delivery: %#v %v", outcome, err)
			}
		}
		return
	}
	tlsConfig, caFile := smtpTestCA(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"smtps", "smtp+starttls"} {
		t.Run(scheme, func(t *testing.T) {
			fixture := smtpServer(t, smtpBehavior{rcptStatus: 250, tlsConfig: tlsConfig, implicitTLS: scheme == "smtps", authUsername: "fixture-operator", authPassword: "fixture-password"})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, executable, "-test.run=^TestSMTPAuthenticatedTLS$", "-test.timeout=8s")
			command.Env = append(os.Environ(), "GOODSENTRY_SMTP_TLS_HELPER=1", "GOODSENTRY_SMTP_TLS_URL="+scheme+"://fixture-operator:fixture-password@"+fixture.address, "SSL_CERT_FILE="+caFile, "SSL_CERT_DIR="+t.TempDir())
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("TLS SMTP helper failed: %v\n%s", err, output)
			}
			for i := 0; i < 2; i++ {
				select {
				case message := <-fixture.messages:
					verifyMail(t, message, "admin@example.com")
				default:
					t.Fatal("helper reported success without DATA received")
				}
			}
			commands := strings.Join(fixture.commands(), "\n")
			if strings.Count(commands, "AUTH PLAIN ") != 2 || strings.Count(commands, "MAIL FROM:<alerts@example.com>") != 2 || strings.Count(commands, "RCPT TO:<admin@example.com>") != 2 {
				t.Fatal("SMTP fixture did not receive authenticated envelope for both deliveries")
			}
			if scheme == "smtp+starttls" && strings.Count(commands, "STARTTLS") != 2 {
				t.Fatal("required STARTTLS was not negotiated for both deliveries")
			}
		})
	}
}

func smtpTestCA(t *testing.T) (*tls.Config, string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	validFrom, validUntil := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "GoodSentry local SMTP test CA"}, NotBefore: validFrom, NotAfter: validUntil, BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: validFrom, NotAfter: validUntil, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}}}, caPath
}
