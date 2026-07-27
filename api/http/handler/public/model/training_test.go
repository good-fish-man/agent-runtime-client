package model

import (
	"strings"
	"testing"

	runtimeentity "github.com/good-fish-man/agent-runtime-client/domain/entity/runtime"
)

func TestSaveAndValidateFineTuneDataset(t *testing.T) {
	input := strings.Join([]string{
		`{"prompt":"hello","completion":"world"}`,
		`{"messages":[{"role":"user","content":"question"},{"role":"assistant","content":"answer"}]}`,
	}, "\n")
	path := t.TempDir() + "/train.jsonl"
	count, err := saveAndValidateDataset(strings.NewReader(input), path, "fine_tune", 10)
	if err != nil {
		t.Fatalf("saveAndValidateDataset() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
}

func TestValidateDistillationRow(t *testing.T) {
	row := trainingDatasetRow{Messages: []runtimeentity.ChatMessage{{Role: "user", Content: "explain this"}}}
	if err := validateTrainingRow(row, "distill"); err != nil {
		t.Fatalf("validateTrainingRow() error = %v", err)
	}
}

func TestNormalizeOllamaName(t *testing.T) {
	if got := normalizeOllamaName(" My Fine Tune! :V1 "); got != "myfinetune:v1" {
		t.Fatalf("normalizeOllamaName() = %q", got)
	}
}

func TestTrainingBaseModelRejectsUnknownModel(t *testing.T) {
	if _, err := trainingBaseModel("private-unknown"); err == nil {
		t.Fatal("trainingBaseModel() expected an error")
	}
}
