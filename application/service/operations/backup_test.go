package operations

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

type memoryArchiver struct {
	dump     []byte
	restored []byte
}

func (a *memoryArchiver) Dump(_ context.Context, output io.Writer) error {
	_, err := output.Write(a.dump)
	return err
}

func (a *memoryArchiver) Restore(_ context.Context, input io.Reader) error {
	value, err := io.ReadAll(input)
	a.restored = value
	return err
}

func (*memoryArchiver) Version(context.Context) (string, error) { return "PostgreSQL 16.4", nil }

func TestEncryptedBackupVerifyAndRestore(t *testing.T) {
	home := t.TempDir()
	keyPath := filepath.Join(home, "backup.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("ab", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("private-database-row\n"), 90000)
	archiver := &memoryArchiver{dump: plain}
	manager := newBackupManagerForTest(filepath.Join(home, "backups"), keyPath, archiver)
	manager.now = func() time.Time { return time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC) }
	manifest, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Status != operationsv1.BackupComplete || !backupIDPattern.MatchString(manifest.BackupID) {
		t.Fatalf("manifest = %+v", manifest)
	}
	ciphertext, err := os.ReadFile(filepath.Join(home, "backups", manifest.BackupID, "database.dump.enc"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("private-database-row")) {
		t.Fatal("plaintext database data was written to the backup artifact")
	}
	verified, err := manager.Verify(context.Background(), manifest.BackupID)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Status != operationsv1.BackupVerified {
		t.Fatalf("verify status = %s", verified.Status)
	}
	request := operationsv1.RestoreRequest{
		Schema: operationsv1.Schema, RestoreID: "restore-1", BackupID: manifest.BackupID,
		TargetVersion: "0.9.0", ExpectedSHA256: manifest.ManifestSHA256,
		Confirmation: "RESTORE " + manifest.BackupID, RequestedBy: "admin", RequestedAt: time.Now().UTC(),
	}
	if _, err := manager.Restore(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(archiver.restored, plain) {
		t.Fatal("restored database stream differs from the original dump")
	}
}

func TestBackupTamperingFailsBeforeRestore(t *testing.T) {
	home := t.TempDir()
	keyPath := filepath.Join(home, "backup.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("cd", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	archiver := &memoryArchiver{dump: []byte("database")}
	manager := newBackupManagerForTest(filepath.Join(home, "backups"), keyPath, archiver)
	manifest, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "backups", manifest.BackupID, "database.dump.enc")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = file.Write([]byte("tamper"))
	_ = file.Close()
	if _, err := manager.Verify(context.Background(), manifest.BackupID); err == nil {
		t.Fatal("tampered backup passed verification")
	}
	if len(archiver.restored) != 0 {
		t.Fatal("restore executed before verification completed")
	}
}
