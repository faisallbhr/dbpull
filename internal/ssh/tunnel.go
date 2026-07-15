package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	"dbpull/internal/config"

	gossh "golang.org/x/crypto/ssh"
)

const defaultKeepAliveInterval = 30 * time.Second

type Tunnel interface {
	Open(ctx context.Context) error
	Close() error
	LocalAddress() string
}

type ClientTunnel struct {
	cfg               config.SSHConfig
	remoteAddr        string
	keepAliveInterval time.Duration

	dialSSH func(network, address string, cfg *gossh.ClientConfig) (sshClient, error)
	listen  func(network, address string) (net.Listener, error)

	mu        sync.Mutex
	client    sshClient
	listener  net.Listener
	localAddr string
	closed    bool
	closeCh   chan struct{}
	wg        sync.WaitGroup
}

type sshClient interface {
	Dial(network, addr string) (net.Conn, error)
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
	Close() error
}

type wrappedSSHClient struct {
	*gossh.Client
}

func NewTunnel(cfg config.SSHConfig, remoteAddr string) *ClientTunnel {
	return &ClientTunnel{
		cfg:               cfg,
		remoteAddr:        remoteAddr,
		keepAliveInterval: defaultKeepAliveInterval,
		dialSSH:           dialSSHClient,
		listen:            net.Listen,
		closeCh:           make(chan struct{}),
	}
}

func (t *ClientTunnel) Open(ctx context.Context) error {
	t.mu.Lock()
	if t.client != nil || t.listener != nil {
		t.mu.Unlock()
		return fmt.Errorf("ssh tunnel already open")
	}
	t.mu.Unlock()

	client, err := t.connect()
	if err != nil {
		return err
	}

	listener, err := t.listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = client.Close()
		return fmt.Errorf("listen on local tunnel port: %w", err)
	}

	t.mu.Lock()
	t.client = client
	t.listener = listener
	t.localAddr = listener.Addr().String()
	t.mu.Unlock()

	t.wg.Add(2)
	go t.serve()
	go t.keepAlive(ctx)

	return nil
}

func (t *ClientTunnel) LocalAddress() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.localAddr
}

func (t *ClientTunnel) Close() error {
	err := t.shutdown()
	t.wg.Wait()
	if err != nil {
		return fmt.Errorf("close ssh tunnel: %w", err)
	}
	return nil
}

func (t *ClientTunnel) shutdown() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	close(t.closeCh)

	listener := t.listener
	client := t.client
	t.listener = nil
	t.client = nil
	t.localAddr = ""
	t.mu.Unlock()

	var err error
	if listener != nil {
		err = listener.Close()
	}

	if client != nil {
		if closeErr := client.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}

func (t *ClientTunnel) connect() (sshClient, error) {
	signer, err := readPrivateKey(t.cfg.PrivateKey)
	if err != nil {
		return nil, err
	}

	clientConfig := &gossh.ClientConfig{
		User:            t.cfg.User,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}

	client, err := t.dialSSH("tcp", net.JoinHostPort(t.cfg.Host, fmt.Sprintf("%d", t.cfg.Port)), clientConfig)
	if err != nil {
		return nil, fmt.Errorf("dial ssh server %q: %w", t.cfg.Host, err)
	}

	return client, nil
}

func (t *ClientTunnel) serve() {
	defer t.wg.Done()

	for {
		listener := t.currentListener()
		if listener == nil {
			return
		}

		localConn, err := listener.Accept()
		if err != nil {
			if t.isClosed() {
				return
			}
			continue
		}

		t.wg.Add(1)
		go t.forward(localConn)
	}
}

func (t *ClientTunnel) forward(localConn net.Conn) {
	defer t.wg.Done()
	defer localConn.Close()

	client := t.currentClient()
	if client == nil {
		return
	}

	remoteConn, err := client.Dial("tcp", t.remoteAddr)
	if err != nil {
		return
	}

	done := make(chan struct{}, 2)
	go proxy(done, remoteConn, localConn)
	go proxy(done, localConn, remoteConn)

	<-done
	_ = localConn.Close()
	_ = remoteConn.Close()
	<-done
}

func (t *ClientTunnel) keepAlive(ctx context.Context) {
	defer t.wg.Done()

	ticker := time.NewTicker(t.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = t.shutdown()
			return
		case <-t.closeCh:
			return
		case <-ticker.C:
			client := t.currentClient()
			if client == nil {
				return
			}
			if _, _, err := client.SendRequest("keepalive@dbpull", true, nil); err != nil {
				_ = t.shutdown()
				return
			}
		}
	}
}

func (t *ClientTunnel) currentClient() sshClient {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.client
}

func (t *ClientTunnel) currentListener() net.Listener {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.listener
}

func (t *ClientTunnel) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

func proxy(done chan<- struct{}, dst io.Writer, src io.Reader) {
	_, _ = io.Copy(dst, src)
	done <- struct{}{}
}

func readPrivateKey(path string) (gossh.Signer, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh private key %q: %w", path, err)
	}

	signer, err := gossh.ParsePrivateKey(key)
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key %q: %w", path, err)
	}

	return signer, nil
}

func dialSSHClient(network, address string, cfg *gossh.ClientConfig) (sshClient, error) {
	client, err := gossh.Dial(network, address, cfg)
	if err != nil {
		return nil, err
	}

	return wrappedSSHClient{Client: client}, nil
}
