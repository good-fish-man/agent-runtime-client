package operations

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/good-fish-man/agent-runtime-client/config"
	"github.com/good-fish-man/agent-runtime-client/types/consts"
	operationsv1 "github.com/good-fish-man/athena-protocol/protocol/operations/v1"
)

const (
	backupMagic     = "ATHENA-BACKUP-AESGCM-2\n"
	backupChunkSize = 1 << 20
	backupProtocol  = "1.0-draft"
	maxManifestSize = 1 << 20
)

var backupIDPattern = regexp.MustCompile(`^backup-[0-9]{8}T[0-9]{6}Z-[a-f0-9]{8}$`)

type DatabaseArchiver interface {
	Dump(context.Context, io.Writer) error
	Restore(context.Context, io.Reader) error
	Version(context.Context) (string, error)
}

type BackupManager struct {
	directory  string
	keyFile    string
	maxBackups int
	archiver   DatabaseArchiver
	now        func() time.Time
	mu         sync.Mutex
}

func NewBackupManager(operationsConfig config.OperationsConfig, databaseConfig config.DBConfig) (*BackupManager, error) {
	if strings.TrimSpace(operationsConfig.BackupDir) == "" || strings.TrimSpace(operationsConfig.EncryptionKeyFile) == "" {
		return nil, nil
	}
	if !filepath.IsAbs(operationsConfig.BackupDir) || !filepath.IsAbs(operationsConfig.EncryptionKeyFile) {
		return nil, fmt.Errorf("backup directory and encryption key file must be absolute paths")
	}
	if operationsConfig.MaxBackups <= 0 {
		operationsConfig.MaxBackups = 10
	}
	archiver := &postgresArchiver{config: databaseConfig, dumpPath: operationsConfig.PGDumpPath, restorePath: operationsConfig.PGRestorePath}
	return &BackupManager{directory: filepath.Clean(operationsConfig.BackupDir), keyFile: filepath.Clean(operationsConfig.EncryptionKeyFile), maxBackups: operationsConfig.MaxBackups, archiver: archiver, now: func() time.Time { return time.Now().UTC() }}, nil
}

func newBackupManagerForTest(directory, keyFile string, archiver DatabaseArchiver) *BackupManager {
	return &BackupManager{directory: directory, keyFile: keyFile, maxBackups: 10, archiver: archiver, now: func() time.Time { return time.Now().UTC() }}
}

func (m *BackupManager) Create(ctx context.Context) (*operationsv1.BackupManifest, error) {
	if m == nil {
		return nil, fmt.Errorf("backup management is not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key, err := readBackupKey(m.keyFile)
	if err != nil {
		return nil, err
	}
	version, err := m.archiver.Version(ctx)
	if err != nil {
		return nil, fmt.Errorf("read PostgreSQL version: %w", err)
	}
	now := m.now().UTC()
	id, err := newBackupID(now)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(m.directory, id)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create backup directory: %w", err)
	}
	artifactPath := filepath.Join(directory, "database.dump.enc")
	file, err := os.OpenFile(artifactPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create encrypted database artifact: %w", err)
	}
	reader, writer := io.Pipe()
	dumpDone := make(chan error, 1)
	go func() {
		dumpErr := m.archiver.Dump(ctx, writer)
		_ = writer.CloseWithError(dumpErr)
		dumpDone <- dumpErr
	}()
	encryptErr := encryptBackup(file, reader, key, id, "database")
	if encryptErr != nil {
		_ = reader.CloseWithError(encryptErr)
	}
	closeErr := file.Close()
	dumpErr := <-dumpDone
	if encryptErr != nil || dumpErr != nil || closeErr != nil {
		_ = os.RemoveAll(directory)
		return nil, joinBackupErrors("create backup", encryptErr, dumpErr, closeErr)
	}
	digest, size, err := fileSHA256(artifactPath)
	if err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	completedAt := m.now().UTC()
	manifest := &operationsv1.BackupManifest{
		Schema: operationsv1.Schema, BackupID: id, SourceVersion: consts.Version, ProtocolVersion: backupProtocol,
		Status: operationsv1.BackupComplete, DatabaseEngine: "postgres", DatabaseVersion: version,
		CreatedAt: now, CompletedAt: &completedAt,
		Artifacts: []operationsv1.BackupArtifact{{Name: "database", RelativePath: "database.dump.enc", SHA256: digest, SizeBytes: size, Classification: operationsv1.ClassificationSensitive, Encrypted: true}},
	}
	if err = sealBackupManifest(manifest, key); err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	if err := manifest.Validate(); err != nil {
		_ = os.RemoveAll(directory)
		return nil, fmt.Errorf("validate backup manifest: %w", err)
	}
	if err := writeBackupManifest(directory, manifest); err != nil {
		_ = os.RemoveAll(directory)
		return nil, err
	}
	if err := m.pruneLocked(); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (m *BackupManager) List() ([]operationsv1.BackupManifest, error) {
	if m == nil {
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.listLocked()
}

func (m *BackupManager) Verify(ctx context.Context, backupID string) (*operationsv1.BackupManifest, error) {
	if m == nil {
		return nil, fmt.Errorf("backup management is not configured")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.verifyLocked(ctx, backupID)
}

func (m *BackupManager) Restore(ctx context.Context, request operationsv1.RestoreRequest) (*operationsv1.BackupManifest, error) {
	if m == nil {
		return nil, fmt.Errorf("backup management is not configured")
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	manifest, err := m.verifyLocked(ctx, request.BackupID)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(request.ExpectedSHA256, manifest.ManifestSHA256) {
		return nil, fmt.Errorf("restore manifest checksum does not match the verified backup")
	}
	if request.ValidateOnly {
		return manifest, nil
	}
	key, err := readBackupKey(m.keyFile)
	if err != nil {
		return nil, err
	}
	artifact, err := databaseBackupArtifact(manifest)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(filepath.Join(m.directory, manifest.BackupID, filepath.FromSlash(artifact.RelativePath)))
	if err != nil {
		return nil, fmt.Errorf("open encrypted restore artifact: %w", err)
	}
	defer file.Close()
	reader, writer := io.Pipe()
	decryptDone := make(chan error, 1)
	go func() {
		decryptErr := decryptBackup(writer, file, key, manifest.BackupID, artifact.Name)
		_ = writer.CloseWithError(decryptErr)
		decryptDone <- decryptErr
	}()
	restoreErr := m.archiver.Restore(ctx, reader)
	if restoreErr != nil {
		_ = reader.CloseWithError(restoreErr)
	}
	decryptErr := <-decryptDone
	if restoreErr != nil || decryptErr != nil {
		return nil, joinBackupErrors("restore backup", restoreErr, decryptErr)
	}
	return manifest, nil
}

func (m *BackupManager) verifyLocked(ctx context.Context, backupID string) (*operationsv1.BackupManifest, error) {
	if !backupIDPattern.MatchString(backupID) {
		return nil, fmt.Errorf("backup id is invalid")
	}
	manifest, err := readBackupManifest(filepath.Join(m.directory, backupID))
	if err != nil {
		return nil, err
	}
	if manifest.BackupID != backupID {
		return nil, fmt.Errorf("backup manifest identity does not match its directory")
	}
	key, err := readBackupKey(m.keyFile)
	if err != nil {
		return nil, err
	}
	if err := authenticateBackupManifest(manifest, key); err != nil {
		return nil, err
	}
	for _, artifact := range manifest.Artifacts {
		path := filepath.Join(m.directory, backupID, filepath.FromSlash(artifact.RelativePath))
		digest, size, err := fileSHA256(path)
		if err != nil || !strings.EqualFold(digest, artifact.SHA256) || size != artifact.SizeBytes {
			return nil, fmt.Errorf("backup artifact %s integrity verification failed", artifact.Name)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		decryptErr := decryptBackup(io.Discard, file, key, manifest.BackupID, artifact.Name)
		_ = file.Close()
		if decryptErr != nil {
			return nil, fmt.Errorf("authenticate backup artifact %s: %w", artifact.Name, decryptErr)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	verified := *manifest
	verified.Status = operationsv1.BackupVerified
	return &verified, nil
}

func (m *BackupManager) listLocked() ([]operationsv1.BackupManifest, error) {
	entries, err := os.ReadDir(m.directory)
	if os.IsNotExist(err) {
		return []operationsv1.BackupManifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	backupEntries := make([]os.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || !backupIDPattern.MatchString(entry.Name()) {
			continue
		}
		backupEntries = append(backupEntries, entry)
	}
	if len(backupEntries) == 0 {
		return []operationsv1.BackupManifest{}, nil
	}
	key, err := readBackupKey(m.keyFile)
	if err != nil {
		return nil, err
	}
	items := make([]operationsv1.BackupManifest, 0, len(backupEntries))
	for _, entry := range backupEntries {
		manifest, err := readBackupManifest(filepath.Join(m.directory, entry.Name()))
		if err != nil {
			return nil, err
		}
		if manifest.BackupID != entry.Name() {
			return nil, fmt.Errorf("backup manifest identity does not match directory %s", entry.Name())
		}
		if err := authenticateBackupManifest(manifest, key); err != nil {
			return nil, fmt.Errorf("authenticate backup %s: %w", entry.Name(), err)
		}
		items = append(items, *manifest)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (m *BackupManager) pruneLocked() error {
	items, err := m.listLocked()
	if err != nil {
		return err
	}
	if len(items) <= m.maxBackups {
		return nil
	}
	for _, item := range items[m.maxBackups:] {
		if !backupIDPattern.MatchString(item.BackupID) {
			return fmt.Errorf("refuse to prune unsafe backup id")
		}
		if err := os.RemoveAll(filepath.Join(m.directory, item.BackupID)); err != nil {
			return fmt.Errorf("prune backup %s: %w", item.BackupID, err)
		}
	}
	return nil
}

func joinBackupErrors(operation string, values ...error) error {
	items := make([]error, 0, len(values))
	for _, value := range values {
		if value != nil {
			items = append(items, value)
		}
	}
	if len(items) == 0 {
		return nil
	}
	return fmt.Errorf("%s: %w", operation, errors.Join(items...))
}

type postgresArchiver struct {
	config      config.DBConfig
	dumpPath    string
	restorePath string
}

func (p *postgresArchiver) Dump(ctx context.Context, output io.Writer) error {
	args := append(p.connectionArgs(), "--format=custom", "--no-owner", "--no-acl", "--dbname", p.config.DBName)
	return p.run(ctx, p.dumpPath, args, nil, output)
}

func (p *postgresArchiver) Restore(ctx context.Context, input io.Reader) error {
	args := append(p.connectionArgs(), "--clean", "--if-exists", "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--dbname", p.config.DBName)
	return p.run(ctx, p.restorePath, args, input, io.Discard)
}

func (p *postgresArchiver) Version(ctx context.Context) (string, error) {
	command := exec.CommandContext(ctx, p.dumpPath, "--version")
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func (p *postgresArchiver) connectionArgs() []string {
	return []string{"--host", p.config.DBHost, "--port", fmt.Sprint(p.config.DBPort), "--username", p.config.Username}
}

func (p *postgresArchiver) run(ctx context.Context, executable string, args []string, input io.Reader, output io.Writer) error {
	command := exec.CommandContext(ctx, executable, args...)
	command.Env = append(os.Environ(), "PGPASSWORD="+p.config.Password)
	command.Stdin, command.Stdout = input, output
	var stderr strings.Builder
	command.Stderr = &limitedWriter{writer: &stderr, remaining: 8192}
	if err := command.Run(); err != nil {
		return fmt.Errorf("%s failed: %w: %s", filepath.Base(executable), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int
}

func (w *limitedWriter) Write(value []byte) (int, error) {
	original := len(value)
	if w.remaining <= 0 {
		return original, nil
	}
	if len(value) > w.remaining {
		value = value[:w.remaining]
	}
	_, err := w.writer.Write(value)
	w.remaining -= len(value)
	return original, err
}

func encryptBackup(output io.Writer, input io.Reader, key []byte, backupID, artifactName string) error {
	gcm, err := backupGCM(key)
	if err != nil {
		return err
	}
	buffered := bufio.NewWriterSize(output, 64<<10)
	if _, err := io.WriteString(buffered, backupMagic); err != nil {
		return err
	}
	buffer := make([]byte, backupChunkSize)
	var sequence uint64
	for {
		count, readErr := input.Read(buffer)
		if count > 0 {
			nonce := make([]byte, gcm.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			sealed := gcm.Seal(nil, nonce, buffer[:count], backupChunkAAD(backupID, artifactName, sequence))
			if err := binary.Write(buffered, binary.BigEndian, uint32(len(sealed))); err != nil {
				return err
			}
			if _, err := buffered.Write(nonce); err != nil {
				return err
			}
			if _, err := buffered.Write(sealed); err != nil {
				return err
			}
			sequence++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	if err := binary.Write(buffered, binary.BigEndian, uint32(0)); err != nil {
		return err
	}
	return buffered.Flush()
}

func decryptBackup(output io.Writer, input io.Reader, key []byte, backupID, artifactName string) error {
	gcm, err := backupGCM(key)
	if err != nil {
		return err
	}
	buffered := bufio.NewReaderSize(input, 64<<10)
	header := make([]byte, len(backupMagic))
	if _, err := io.ReadFull(buffered, header); err != nil || string(header) != backupMagic {
		return fmt.Errorf("backup encryption header is invalid")
	}
	var sequence uint64
	for {
		var size uint32
		if err := binary.Read(buffered, binary.BigEndian, &size); err != nil {
			return err
		}
		if size == 0 {
			return nil
		}
		if size > backupChunkSize+uint32(gcm.Overhead()) {
			return fmt.Errorf("encrypted backup chunk exceeds limit")
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := io.ReadFull(buffered, nonce); err != nil {
			return err
		}
		sealed := make([]byte, size)
		if _, err := io.ReadFull(buffered, sealed); err != nil {
			return err
		}
		plain, err := gcm.Open(nil, nonce, sealed, backupChunkAAD(backupID, artifactName, sequence))
		if err != nil {
			return err
		}
		if _, err := output.Write(plain); err != nil {
			return err
		}
		sequence++
	}
}

func backupChunkAAD(backupID, artifactName string, sequence uint64) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%d", backupProtocol, backupID, artifactName, sequence))
}

func backupGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func readBackupKey(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect backup encryption key: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("backup encryption key must be a regular file")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("backup encryption key permissions must not allow group or other access")
	}
	if info.Size() > 128 {
		return nil, fmt.Errorf("backup encryption key file exceeds the size limit")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read backup encryption key: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("backup encryption key changed while it was being opened")
	}
	data, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil {
		return nil, fmt.Errorf("read backup encryption key: %w", err)
	}
	value := strings.TrimSpace(string(data))
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 32 {
		return nil, fmt.Errorf("backup encryption key must be 32 bytes encoded as 64 hexadecimal characters")
	}
	return decoded, nil
}

func newBackupID(now time.Time) (string, error) {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "backup-" + now.Format("20060102T150405Z") + "-" + hex.EncodeToString(random), nil
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func backupManifestDigest(manifest *operationsv1.BackupManifest) (string, error) {
	copy := *manifest
	copy.ManifestSHA256 = ""
	copy.Integrity.Value = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func sealBackupManifest(manifest *operationsv1.BackupManifest, key []byte) error {
	manifest.Integrity = operationsv1.IntegrityProof{
		Algorithm: operationsv1.IntegrityHMACSHA256,
		KeyID:     backupKeyID(key),
	}
	digest, err := backupManifestDigest(manifest)
	if err != nil {
		return err
	}
	manifest.ManifestSHA256 = digest
	manifest.Integrity.Value, err = backupManifestMAC(manifest, key)
	return err
}

func backupManifestMAC(manifest *operationsv1.BackupManifest, key []byte) (string, error) {
	copy := *manifest
	copy.Integrity.Value = ""
	data, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func verifyBackupManifestMAC(manifest *operationsv1.BackupManifest, key []byte) error {
	if manifest.Integrity.Algorithm != operationsv1.IntegrityHMACSHA256 || manifest.Integrity.KeyID != backupKeyID(key) {
		return fmt.Errorf("backup manifest integrity key does not match this installation")
	}
	expected, err := backupManifestMAC(manifest, key)
	if err != nil {
		return err
	}
	expectedBytes, expectedErr := hex.DecodeString(expected)
	actualBytes, actualErr := hex.DecodeString(manifest.Integrity.Value)
	if expectedErr != nil || actualErr != nil || !hmac.Equal(expectedBytes, actualBytes) {
		return fmt.Errorf("backup manifest authentication failed")
	}
	return nil
}

func authenticateBackupManifest(manifest *operationsv1.BackupManifest, key []byte) error {
	if err := manifest.Validate(); err != nil {
		return fmt.Errorf("validate backup manifest: %w", err)
	}
	expected, err := backupManifestDigest(manifest)
	if err != nil || !strings.EqualFold(expected, manifest.ManifestSHA256) {
		return fmt.Errorf("backup manifest integrity verification failed")
	}
	return verifyBackupManifestMAC(manifest, key)
}

func backupKeyID(key []byte) string {
	digest := sha256.Sum256(key)
	return "sha256:" + hex.EncodeToString(digest[:8])
}

func databaseBackupArtifact(manifest *operationsv1.BackupManifest) (operationsv1.BackupArtifact, error) {
	var result operationsv1.BackupArtifact
	count := 0
	for _, artifact := range manifest.Artifacts {
		if artifact.Name == "database" {
			result = artifact
			count++
		}
	}
	if count != 1 {
		return operationsv1.BackupArtifact{}, fmt.Errorf("backup must contain exactly one database artifact")
	}
	return result, nil
}

func writeBackupManifest(directory string, manifest *operationsv1.BackupManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "manifest.json")
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func readBackupManifest(directory string) (*operationsv1.BackupManifest, error) {
	path := filepath.Join(directory, "manifest.json")
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("read backup manifest: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxManifestSize {
		return nil, fmt.Errorf("backup manifest must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read backup manifest: %w", err)
	}
	defer file.Close()
	var manifest operationsv1.BackupManifest
	decoder := json.NewDecoder(io.LimitReader(file, maxManifestSize+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("parse backup manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("backup manifest contains trailing data")
	}
	return &manifest, nil
}
