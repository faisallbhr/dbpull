package ssh

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dbpull/internal/config"

	gossh "golang.org/x/crypto/ssh"
)

func TestOpenAllocatesLocalPortAndForwardsTraffic(t *testing.T) {
	tunnel := newTestTunnel(t)
	listener := newFakeListener("127.0.0.1:40123")
	tunnel.listen = func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	tunnel.dialSSH = func(network, address string, cfg *gossh.ClientConfig) (sshClient, error) {
		return &fakeSSHClient{
			dial: func(network, addr string) (net.Conn, error) {
				serverConn, clientConn := net.Pipe()
				go func() {
					defer serverConn.Close()
					buf := make([]byte, 4)
					_, _ = io.ReadFull(serverConn, buf)
					_, _ = serverConn.Write([]byte("pong"))
				}()
				return clientConn, nil
			},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() {
		if err := tunnel.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}()

	if tunnel.LocalAddress() == "" {
		t.Fatal("LocalAddress() is empty")
	}

	serverConn, conn := net.Pipe()
	defer conn.Close()
	defer serverConn.Close()

	listener.accept(serverConn)

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("conn.Write() error = %v", err)
	}

	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil {
		t.Fatalf("io.ReadFull() error = %v", err)
	}

	if string(reply) != "pong" {
		t.Fatalf("reply = %q, want %q", string(reply), "pong")
	}
}

func TestKeepAliveSendsRequests(t *testing.T) {
	var keepAliveCount atomic.Int32

	tunnel := newTestTunnel(t)
	tunnel.listen = func(network, address string) (net.Listener, error) {
		return newFakeListener("127.0.0.1:40123"), nil
	}
	tunnel.keepAliveInterval = 10 * time.Millisecond
	tunnel.dialSSH = func(network, address string, cfg *gossh.ClientConfig) (sshClient, error) {
		return &fakeSSHClient{
			sendRequest: func(name string, wantReply bool, payload []byte) (bool, []byte, error) {
				if name == "keepalive@dbpull" {
					keepAliveCount.Add(1)
				}
				return true, nil, nil
			},
		}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer func() { _ = tunnel.Close() }()

	deadline := time.Now().Add(200 * time.Millisecond)
	for keepAliveCount.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if keepAliveCount.Load() == 0 {
		t.Fatal("keepalive was not sent")
	}
}

func TestContextCancellationClosesTunnel(t *testing.T) {
	tunnel := newTestTunnel(t)
	listener := newFakeListener("127.0.0.1:40123")
	tunnel.listen = func(network, address string) (net.Listener, error) {
		return listener, nil
	}
	tunnel.dialSSH = func(network, address string, cfg *gossh.ClientConfig) (sshClient, error) {
		return &fakeSSHClient{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	if err := tunnel.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	localAddr := tunnel.LocalAddress()
	cancel()

	deadline := time.Now().Add(200 * time.Millisecond)
	for tunnel.LocalAddress() != "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if tunnel.LocalAddress() != "" {
		t.Fatal("LocalAddress() still set after context cancellation")
	}

	select {
	case <-listener.closed:
	default:
		t.Fatalf("listener for %q is still open after context cancellation", localAddr)
	}
}

func TestOpenReturnsErrorWhenPrivateKeyIsMissing(t *testing.T) {
	tunnel := NewTunnel(config.SSHConfig{
		Host:       "example.com",
		Port:       22,
		User:       "deploy",
		PrivateKey: filepath.Join(t.TempDir(), "missing"),
	}, "127.0.0.1:3306")

	err := tunnel.Open(context.Background())
	if err == nil {
		t.Fatal("Open() error = nil, want error")
	}

	if got := err.Error(); got == "" || !strings.Contains(got, `read ssh private key`) {
		t.Fatalf("Open() error = %q", got)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	tunnel := newTestTunnel(t)
	tunnel.listen = func(network, address string) (net.Listener, error) {
		return newFakeListener("127.0.0.1:40123"), nil
	}
	tunnel.dialSSH = func(network, address string, cfg *gossh.ClientConfig) (sshClient, error) {
		return &fakeSSHClient{}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := tunnel.Open(ctx); err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := tunnel.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}

	if err := tunnel.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func newTestTunnel(t *testing.T) *ClientTunnel {
	t.Helper()

	return NewTunnel(config.SSHConfig{
		Host:       "example.com",
		Port:       22,
		User:       "deploy",
		PrivateKey: writePrivateKey(t),
	}, "127.0.0.1:3306")
}

func writePrivateKey(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}

	privateKey := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	path := filepath.Join(t.TempDir(), "id_rsa")
	if err := os.WriteFile(path, privateKey, 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}

	return path
}

type fakeSSHClient struct {
	dial        func(network, addr string) (net.Conn, error)
	sendRequest func(name string, wantReply bool, payload []byte) (bool, []byte, error)
	close       func() error
}

type fakeListener struct {
	addr   net.Addr
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func newFakeListener(address string) *fakeListener {
	return &fakeListener{
		addr:   fakeAddr(address),
		conns:  make(chan net.Conn, 1),
		closed: make(chan struct{}),
	}
}

func (l *fakeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *fakeListener) Close() error {
	l.once.Do(func() {
		close(l.closed)
	})
	return nil
}

func (l *fakeListener) Addr() net.Addr {
	return l.addr
}

func (l *fakeListener) accept(conn net.Conn) {
	l.conns <- conn
}

type fakeAddr string

func (a fakeAddr) Network() string {
	return "tcp"
}

func (a fakeAddr) String() string {
	return string(a)
}

func (c *fakeSSHClient) Dial(network, addr string) (net.Conn, error) {
	if c.dial != nil {
		return c.dial(network, addr)
	}
	return nil, errors.New("dial not implemented")
}

func (c *fakeSSHClient) SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error) {
	if c.sendRequest != nil {
		return c.sendRequest(name, wantReply, payload)
	}
	return true, nil, nil
}

func (c *fakeSSHClient) Close() error {
	if c.close != nil {
		return c.close()
	}
	return nil
}
