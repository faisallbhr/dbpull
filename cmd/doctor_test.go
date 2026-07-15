package cmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbpull/internal/config"
	"dbpull/internal/db"
	"dbpull/internal/ssh"
)

func TestDoctorSuccess(t *testing.T) {
	restore := stubDoctorDeps(t)
	defer restore()

	keyPath := writeTempFile(t, "id_rsa", "key")
	loadConfig = func(path string) (config.Config, error) {
		return config.Config{
			SSH: config.SSHConfig{PrivateKey: keyPath},
			Source: config.SourceConfig{
				Database: "source",
			},
			Target: config.TargetConfig{
				Host: "localhost",
			},
		}, nil
	}
	newTunnel = func(cfg config.SSHConfig, remoteAddr string) ssh.Tunnel {
		return sshTunnelStub{localAddress: "127.0.0.1:3307"}
	}
	connectSource = func(cfg config.SourceConfig, tunnelAddr string) (sourceClient, error) {
		return doctorSourceStub{}, nil
	}
	connectTarget = func(cfg config.TargetConfig) (targetClient, error) {
		return doctorTargetStub{}, nil
	}

	cmd := newDoctorCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetContext(context.Background())

	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := []string{
		"✓ SSH key",
		"✓ SSH connection",
		"✓ Tunnel",
		"✓ Source database",
		"✓ Target database",
	}
	for _, line := range want {
		if !strings.Contains(out.String(), line) {
			t.Fatalf("output = %q, want line %q", out.String(), line)
		}
	}
}

func TestDoctorReturnsError(t *testing.T) {
	restore := stubDoctorDeps(t)
	defer restore()

	loadConfig = func(path string) (config.Config, error) {
		return config.Config{}, errors.New("boom")
	}

	cmd := newDoctorCmd()
	cmd.SetContext(context.Background())

	err := cmd.Execute()
	if err == nil {
		t.Fatal("Execute() error = nil, want error")
	}
}

func stubDoctorDeps(t *testing.T) func() {
	t.Helper()

	prevLoadConfig := loadConfig
	prevNewTunnel := newTunnel
	prevConnectSource := connectSource
	prevConnectTarget := connectTarget

	return func() {
		loadConfig = prevLoadConfig
		newTunnel = prevNewTunnel
		connectSource = prevConnectSource
		connectTarget = prevConnectTarget
	}
}

type sshTunnelStub struct {
	localAddress string
}

func (s sshTunnelStub) Open(ctx context.Context) error {
	return nil
}

func (s sshTunnelStub) Close() error {
	return nil
}

func (s sshTunnelStub) LocalAddress() string {
	return s.localAddress
}

type doctorSourceStub struct{}

func (doctorSourceStub) Close() error {
	return nil
}

func (doctorSourceStub) Ping(ctx context.Context) error {
	return nil
}

func (doctorSourceStub) ListTables(ctx context.Context) ([]db.Table, error) {
	return nil, nil
}

func (doctorSourceStub) ShowCreateTable(ctx context.Context, table string) (string, error) {
	return "", nil
}

func (doctorSourceStub) StreamRows(
	ctx context.Context,
	table string,
	batchSize int,
	notice db.BatchSizeNotice,
	handle db.RowBatchHandler,
) error {
	return nil
}

type doctorTargetStub struct{}

func (doctorTargetStub) Close() error {
	return nil
}

func (doctorTargetStub) Ping(ctx context.Context) error {
	return nil
}

func (doctorTargetStub) DisableForeignKeyChecks(ctx context.Context) error {
	return nil
}

func (doctorTargetStub) EnableForeignKeyChecks(ctx context.Context) error {
	return nil
}

func (doctorTargetStub) DropTable(ctx context.Context, table string) error {
	return nil
}

func (doctorTargetStub) CreateTable(ctx context.Context, createSQL string) error {
	return nil
}

func (doctorTargetStub) InsertBatch(ctx context.Context, table string, batch db.RowBatch) error {
	return nil
}

func writeTempFile(t *testing.T, name, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v", err)
	}
	return path
}
