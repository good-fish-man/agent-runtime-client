package operations

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
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
	listed, err := manager.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Status != operationsv1.BackupVerified || listed[0].ManifestSHA256 != verified.ManifestSHA256 {
		t.Fatalf("verified backup status was not persisted: %+v", listed)
	}
	request := operationsv1.RestoreRequest{
		Schema: operationsv1.Schema, RestoreID: "restore-1", BackupID: manifest.BackupID,
		TargetVersion: "0.9.0", ExpectedSHA256: verified.ManifestSHA256,
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
	if _, err := manager.List(); err == nil {
		t.Fatal("tampered backup passed inventory integrity verification")
	}
	if len(archiver.restored) != 0 {
		t.Fatal("restore executed before verification completed")
	}
}

func TestBackupManifestCannotBeRewrittenWithOnlySHA256(t *testing.T) {
	home := t.TempDir()
	keyPath := filepath.Join(home, "backup.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("ef", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newBackupManagerForTest(filepath.Join(home, "backups"), keyPath, &memoryArchiver{dump: []byte("database")})
	manifest, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "backups", manifest.BackupID, "manifest.json")
	forged, err := readBackupManifest(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	forged.DatabaseVersion = "PostgreSQL attacker"
	forged.ManifestSHA256, err = backupManifestDigest(forged)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeBackupManifest(filepath.Dir(path), forged); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(context.Background(), manifest.BackupID); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("forged manifest was not rejected by HMAC: %v", err)
	}
	if _, err := manager.List(); err == nil || !strings.Contains(err.Error(), "authentication failed") {
		t.Fatalf("backup inventory accepted forged metadata: %v", err)
	}
}

func TestBackupManifestCannotMoveBetweenBackupIDs(t *testing.T) {
	home := t.TempDir()
	keyPath := filepath.Join(home, "backup.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("12", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := newBackupManagerForTest(filepath.Join(home, "backups"), keyPath, &memoryArchiver{dump: []byte("database")})
	manifest, err := manager.Create(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	otherID := "backup-20260815T120001Z-deadbeef"
	otherDir := filepath.Join(home, "backups", otherID)
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, "backups", manifest.BackupID, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Verify(context.Background(), otherID); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("relocated manifest was not rejected: %v", err)
	}
}

func TestBackupKeyRejectsUnsafePermissionsAndSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX key permission semantics are not available on Windows")
	}
	home := t.TempDir()
	keyPath := filepath.Join(home, "backup.key")
	if err := os.WriteFile(keyPath, []byte(strings.Repeat("34", 32)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readBackupKey(keyPath); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("unsafe key permissions were accepted: %v", err)
	}
	if err := os.Chmod(keyPath, 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(home, "backup-link.key")
	if err := os.Symlink(keyPath, linkPath); err != nil {
		t.Fatal(err)
	}
	if _, err := readBackupKey(linkPath); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("symlinked key was accepted: %v", err)
	}
}
