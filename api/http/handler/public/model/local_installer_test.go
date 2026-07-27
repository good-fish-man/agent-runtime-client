package model

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseDownloadSize(t *testing.T) {
	if got, want := parseDownloadSize("7GB"), uint64(7<<30); got != want {
		t.Fatalf("parseDownloadSize() = %d, want %d", got, want)
	}
}

func TestFindExistingFileSkipsMissingPathsAndDirectories(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "bin", "ollama")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := findExistingFile(filepath.Join(root, "missing"), root, file)
	if err != nil {
		t.Fatal(err)
	}
	if got != file {
		t.Fatalf("findExistingFile() = %q, want %q", got, file)
	}
}

func TestFindExistingFileReturnsNotFound(t *testing.T) {
	_, err := findExistingFile(filepath.Join(t.TempDir(), "missing"))
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("findExistingFile() error = %v, want exec.ErrNotFound", err)
	}
}

func TestCleanupUnusedDiffusersArtifacts(t *testing.T) {
	root := t.TempDir()
	files := []string{
		".cache/huggingface/download/model.fp16.safetensors.incomplete",
		"model.onnx",
		"model.onnx_data",
		"openvino_model.bin",
		"flax_model.msgpack",
		"preview.png",
		"unet/diffusion_pytorch_model.bin",
		"unet/diffusion_pytorch_model.safetensors",
		"unet/diffusion_pytorch_model.fp16.safetensors",
		"config.json",
	}
	for _, name := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanupUnusedDiffusersArtifacts(root, "stabilityai/stable-diffusion-xl-base-1.0"); err != nil {
		t.Fatal(err)
	}

	removed := files[:8]
	for _, name := range removed {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed", name)
		}
	}
	for _, name := range files[8:] {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Errorf("expected %s to remain: %v", name, err)
		}
	}
}
