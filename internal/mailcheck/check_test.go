package mailcheck

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func TestAuthenticatedChecksNeverAccessOrSendMail(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "mail.hust.edu.cn"}, DNSNames: []string{"mail.hust.edu.cn"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	for _, mode := range []string{"success", "auth-rejected", "untrusted", "canceled", "connect-failed"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "canceled" {
				cancel()
			}
			done := make(chan struct{}, 2)
			dial := func(ctx context.Context, network, address string) (net.Conn, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if mode == "connect-failed" {
					return nil, errors.New("secret-marker")
				}
				client, server := net.Pipe()
				go func() {
					defer func() { done <- struct{}{} }()
					defer server.Close()
					_ = server.SetDeadline(time.Now().Add(3 * time.Second))
					conn := tls.Server(server, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
					if err := conn.Handshake(); err != nil {
						return
					}
					smtp := strings.HasSuffix(address, ":465")
					if smtp {
						fmt.Fprint(conn, "220 test\r\n")
					} else {
						fmt.Fprint(conn, "* OK ready\r\n")
					}
					scanner := bufio.NewScanner(conn)
					for scanner.Scan() {
						fields := strings.Fields(scanner.Text())
						if len(fields) == 0 || (!smtp && len(fields) < 2) {
							t.Error("invalid command")
							return
						}
						if smtp {
							switch fields[0] {
							case "EHLO":
								fmt.Fprint(conn, "250-test\r\n250 AUTH PLAIN\r\n")
							case "AUTH":
								if mode == "auth-rejected" {
									fmt.Fprint(conn, "535 secret-marker\r\n")
								} else {
									fmt.Fprint(conn, "235 authenticated\r\n")
								}
							case "*":
								fmt.Fprint(conn, "501 authentication canceled\r\n")
							case "QUIT":
								fmt.Fprint(conn, "221 bye\r\n")
								return // net/smtp Auth failure cleanup
							default:
								t.Errorf("forbidden SMTP command %s", fields[0])
								return
							}
						} else {
							switch fields[1] {
							case "CAPABILITY":
								fmt.Fprintf(conn, "* CAPABILITY IMAP4rev1\r\n%s OK capability\r\n", fields[0])
							case "LOGIN":
								if mode == "auth-rejected" {
									fmt.Fprintf(conn, "%s NO secret-marker\r\n", fields[0])
								} else {
									fmt.Fprintf(conn, "%s OK login\r\n", fields[0])
								}
							default:
								t.Errorf("forbidden IMAP command %s", fields[1])
								return
							}
						}
					}
				}()
				return client, nil
			}
			cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "mail.hust.edu.cn", RootCAs: roots}
			if mode == "untrusted" {
				cfg.RootCAs = x509.NewCertPool()
			}
			results, err := check(ctx, Options{User: "test@hust.edu.cn", Password: "secret-marker", Timeout: time.Second}, dial, cfg)
			if err != nil || len(results) != 2 {
				t.Fatalf("results=%v err=%v", results, err)
			}
			for _, r := range results {
				wantStage := "auth"
				if mode == "untrusted" {
					wantStage = "tls"
				}
				if mode == "canceled" || mode == "connect-failed" {
					wantStage = "connect"
				}
				if r.OK != (mode == "success") || r.Stage != wantStage || strings.Contains(r.Message, "secret-marker") {
					t.Errorf("unexpected result: %+v", r)
				}
			}
			if mode != "canceled" && mode != "connect-failed" {
				for range 2 {
					select {
					case <-done:
					case <-time.After(4 * time.Second):
						t.Fatal("peer did not stop")
					}
				}
			}
		})
	}
}

func TestTimeoutClosesBlockedGreeting(t *testing.T) {
	// A stalled TCP peer must also be interruptible during TLS negotiation.
	done := make(chan struct{}, 2)
	dial := func(context.Context, string, string) (net.Conn, error) {
		c, s := net.Pipe()
		go func() {
			defer s.Close()
			var b [4096]byte
			for {
				if _, err := s.Read(b[:]); err != nil {
					done <- struct{}{}
					return
				}
			}
		}()
		return c, nil
	}
	start := time.Now()
	results, err := check(context.Background(), Options{User: "test", Password: "secret", Timeout: 20 * time.Millisecond}, dial, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "mail.hust.edu.cn"})
	if err != nil || len(results) != 2 || results[0].OK || results[1].OK || time.Since(start) > time.Second {
		t.Fatalf("timeout results=%v err=%v", results, err)
	}
	for range 2 {
		<-done
	}
}

func TestInvalidOptionsDoNotDial(t *testing.T) {
	for _, o := range []Options{{}, {User: "test", Password: "secret"}, {User: "test", Timeout: time.Second}} {
		_, err := check(context.Background(), o, func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("unexpected network")
			return nil, nil
		}, nil)
		if err == nil {
			t.Fatal("accepted invalid options")
		}
	}
}
