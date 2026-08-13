package model

import (
	"testing"

	dto "github.com/good-fish-man/agent-runtime-client/application/dto/model"
	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/model"
)

func TestMaskKey(t *testing.T) {
	tests := map[string]string{
		"":              "未设置",
		"short":         "********",
		"sk-1234567890": "sk-1...7890",
	}
	for input, want := range tests {
		if got := maskKey(input); got != want {
			t.Fatalf("maskKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestApplyModelUsageMetrics(t *testing.T) {
	items := []*dto.FindSysModelRsp{
		{Ulid: "model-1", CreatedBy: "user-1", Name: "gpt-5", Latency: "N/A"},
		{Ulid: "model-2", CreatedBy: "user-1", Name: "gpt-mini", Latency: "N/A"},
		{Ulid: "model-3", CreatedBy: "user-2", Name: "image-model", Latency: "N/A"},
		{Ulid: "model-4", CreatedBy: "user-1", Name: "sub-agent-model", Latency: "N/A"},
	}
	metrics := []entity.ModelUsageMetric{
		{UserID: "user-1", ModelID: "model-1", ModelName: "gpt-5", RequestCount: 3, SuccessCount: 2, InputTokens: 1200, OutputTokens: 300, TotalTokens: 1500, LatencyTotalMs: 3000, LatencyCount: 3},
		// Historical records did not have model_id, so a unique model name remains a safe fallback.
		{UserID: "user-1", ModelName: "gpt-mini", RequestCount: 1, SuccessCount: 1, InputTokens: 200, OutputTokens: 50, TotalTokens: 250, LatencyTotalMs: 500, LatencyCount: 1},
		{UserID: "user-2", ModelID: "model-3", ModelName: "image-model", RequestCount: 2, SuccessCount: 1, LatencyTotalMs: 10000, LatencyCount: 2},
		{UserID: "user-1", ModelID: "model-4", ModelName: "sub-agent-model", InputTokens: 400, OutputTokens: 100, TotalTokens: 500},
	}

	applyModelUsageMetrics(items, metrics)

	assertModelMetric(t, items[0], 3, 75, 66.7, "1.00 s")
	assertModelMetric(t, items[1], 1, 25, 100, "500 ms")
	assertModelMetric(t, items[2], 2, 100, 50, "5.00 s")
	assertModelTokens(t, items[0], 1200, 300, 1500)
	assertModelTokens(t, items[1], 200, 50, 250)
	assertModelTokens(t, items[2], 0, 0, 0)
	assertModelMetric(t, items[3], 0, 0, 0, "N/A")
	assertModelTokens(t, items[3], 400, 100, 500)
}

func assertModelTokens(t *testing.T, item *dto.FindSysModelRsp, input, output, total int64) {
	t.Helper()
	if item.InputTokens != input || item.OutputTokens != output || item.TotalTokens != total {
		t.Fatalf("unexpected token metric for %s: input=%d output=%d total=%d", item.Ulid, item.InputTokens, item.OutputTokens, item.TotalTokens)
	}
}

func TestApplyModelUsageMetricsDoesNotGuessDuplicateNames(t *testing.T) {
	items := []*dto.FindSysModelRsp{
		{Ulid: "model-1", CreatedBy: "user-1", Name: "shared-name"},
		{Ulid: "model-2", CreatedBy: "user-1", Name: "shared-name"},
	}

	applyModelUsageMetrics(items, []entity.ModelUsageMetric{{
		UserID: "user-1", ModelName: "shared-name", RequestCount: 4, SuccessCount: 4,
	}})

	for _, item := range items {
		if item.UsageCount != 0 || item.UsageRate != 0 {
			t.Fatalf("ambiguous historical metric was assigned to %s: %#v", item.Ulid, item)
		}
	}
}

func assertModelMetric(t *testing.T, item *dto.FindSysModelRsp, count int64, rate, success float64, latency string) {
	t.Helper()
	if item.UsageCount != count || item.UsageRate != rate || item.SuccessRate != success || item.Latency != latency {
		t.Fatalf("unexpected metric for %s: count=%d rate=%v success=%v latency=%q", item.Ulid, item.UsageCount, item.UsageRate, item.SuccessRate, item.Latency)
	}
}
