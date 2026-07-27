package knowledge_base

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	entity "github.com/good-fish-man/agent-runtime-client/domain/entity/knowledge_base"
	"github.com/good-fish-man/agent-runtime-client/infra/data"
	repo "github.com/good-fish-man/agent-runtime-client/infra/repository/repo/knowledge_base"
)

// KnowledgeRetrievalSvc calls configured external knowledge retrieval services.
type KnowledgeRetrievalSvc struct {
	kbRepo *repo.SysKnowledgeBaseRepo
}

// NewKnowledgeRetrievalSvc builds the retrieval service over the shared data handle.
func NewKnowledgeRetrievalSvc(d *data.Data) *KnowledgeRetrievalSvc {
	return &KnowledgeRetrievalSvc{kbRepo: repo.NewSysKnowledgeBaseRepo(d)}
}

// KnowledgeRetrievalResult is one retrieval hit.
type KnowledgeRetrievalResult struct {
	Title   string  `json:"title"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
	KbName  string  `json:"kb_name"`
}

// RetrievalConfig identifies a KB and the number of hits to retrieve.
type RetrievalConfig struct {
	KbId string `json:"kb_id"`
	TopK int    `json:"top_k"`
}

// RecallFromKnowledgeBases recalls from multiple knowledge bases; one KB failure does not fail all.
func (s *KnowledgeRetrievalSvc) RecallFromKnowledgeBases(ctx context.Context, configs []RetrievalConfig, query string) ([]KnowledgeRetrievalResult, error) {
	if len(configs) == 0 {
		return nil, nil
	}
	var allResults []KnowledgeRetrievalResult
	for _, cfg := range configs {
		results, err := s.RecallFromSingleKnowledgeBase(ctx, cfg.KbId, query, cfg.TopK)
		if err != nil {
			continue
		}
		allResults = append(allResults, results...)
	}
	return allResults, nil
}

// RecallFromSingleKnowledgeBase recalls from one configured knowledge base.
func (s *KnowledgeRetrievalSvc) RecallFromSingleKnowledgeBase(ctx context.Context, kbId string, query string, topK int) ([]KnowledgeRetrievalResult, error) {
	if topK <= 0 {
		topK = 5
	}
	kb, err := s.kbRepo.FindById(ctx, kbId)
	if err != nil {
		return nil, fmt.Errorf("failed to find knowledge base: %w", err)
	}
	if kb == nil || kb.Ulid == "" || kb.DeletedAt != 0 {
		return nil, fmt.Errorf("knowledge base not found or deleted: %s", kbId)
	}
	if !kb.Enabled {
		return nil, fmt.Errorf("knowledge base disabled: %s", kbId)
	}
	if kb.RetrievalUrl == "" {
		return nil, fmt.Errorf("retrieval url is empty for kb: %s", kbId)
	}
	return s.callRetrievalService(ctx, kb, query, topK)
}

func (s *KnowledgeRetrievalSvc) callRetrievalService(ctx context.Context, kb *entity.SysKnowledgeBase, query string, topK int) ([]KnowledgeRetrievalResult, error) {
	reqBody, err := json.Marshal(map[string]any{"query": query, "top_k": topK})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal recall request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, kb.RetrievalUrl, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if kb.Token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+kb.Token)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to call retrieval service: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("retrieval service returned status %d: %s", resp.StatusCode, string(body))
	}

	results, err := parseRetrievalResults(body)
	if err != nil {
		return nil, err
	}
	for i := range results {
		if results[i].KbName == "" {
			results[i].KbName = kb.Name
		}
	}
	return results, nil
}

func parseRetrievalResults(body []byte) ([]KnowledgeRetrievalResult, error) {
	var results []KnowledgeRetrievalResult
	if err := json.Unmarshal(body, &results); err == nil {
		return results, nil
	}
	var wrapped struct {
		Data    []KnowledgeRetrievalResult `json:"data"`
		Results []KnowledgeRetrievalResult `json:"results"`
	}
	if err := json.Unmarshal(body, &wrapped); err == nil {
		if wrapped.Results != nil {
			return wrapped.Results, nil
		}
		if wrapped.Data != nil {
			return wrapped.Data, nil
		}
	}
	var single KnowledgeRetrievalResult
	if err := json.Unmarshal(body, &single); err == nil {
		return []KnowledgeRetrievalResult{single}, nil
	}
	return nil, fmt.Errorf("failed to parse retrieval response")
}

// FormatKnowledgeContext formats retrieval hits for prompt injection.
func (s *KnowledgeRetrievalSvc) FormatKnowledgeContext(results []KnowledgeRetrievalResult) string {
	if len(results) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n【相关知识】\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. [%s] %s\n", i+1, r.KbName, r.Title))
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.Content))
	}
	return sb.String()
}
