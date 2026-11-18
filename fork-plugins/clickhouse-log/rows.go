package main

import (
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const clickhouseTimeLayout = "2006-01-02 15:04:05.000"

type promptRow struct {
	RequestID      string `json:"request_id"`
	TraceID        string `json:"trace_id"`
	StartedAt      string `json:"started_at"`
	CompletedAt    string `json:"completed_at"`
	SourceFormat   string `json:"source_format"`
	Model          string `json:"model"`
	RequestedModel string `json:"requested_model"`
	Stream         bool   `json:"stream"`
	Outcome        string `json:"outcome"`
	StatusCode     int    `json:"status_code"`
	Error          string `json:"error"`
	RequestBody    string `json:"request_body"`
	ResponseBody   string `json:"response_body"`
}

type usageRow struct {
	RequestID           string `json:"request_id"`
	TraceID             string `json:"trace_id"`
	RequestedAt         string `json:"requested_at"`
	Provider            string `json:"provider"`
	ExecutorType        string `json:"executor_type"`
	Model               string `json:"model"`
	ResponseModel       string `json:"response_model"`
	Alias               string `json:"alias"`
	ClientKey           string `json:"client_key"`
	AuthID              string `json:"auth_id"`
	AuthType            string `json:"auth_type"`
	Source              string `json:"source"`
	ReasoningEffort     string `json:"reasoning_effort"`
	ServiceTier         string `json:"service_tier"`
	Stream              bool   `json:"stream"`
	Failed              bool   `json:"failed"`
	LatencyMs           int64  `json:"latency_ms"`
	TTFTMs              int64  `json:"ttft_ms"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	ReasoningTokens     int64  `json:"reasoning_tokens"`
	CachedTokens        int64  `json:"cached_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
}

func newPromptRow(completion pluginapi.RequestCompletion, captured exchange) promptRow {
	return promptRow{
		RequestID:      completion.RequestID,
		TraceID:        completion.TraceID,
		StartedAt:      clickhouseTime(completion.StartedAt),
		CompletedAt:    clickhouseTime(completion.CompletedAt),
		SourceFormat:   completion.SourceFormat,
		Model:          completion.Model,
		RequestedModel: completion.RequestedModel,
		Stream:         completion.Stream,
		Outcome:        string(completion.Outcome),
		StatusCode:     completion.StatusCode,
		Error:          completion.Error,
		RequestBody:    string(captured.requestBody),
		ResponseBody:   string(captured.responseBody),
	}
}

func newUsageRow(record pluginapi.UsageRecord) usageRow {
	detail := record.Detail
	return usageRow{
		RequestID:           record.RequestID,
		TraceID:             record.TraceID,
		RequestedAt:         clickhouseTime(record.RequestedAt),
		Provider:            record.Provider,
		ExecutorType:        record.ExecutorType,
		Model:               record.Model,
		ResponseModel:       record.ResponseModel,
		Alias:               record.Alias,
		ClientKey:           maskSecret(record.APIKey),
		AuthID:              record.AuthID,
		AuthType:            record.AuthType,
		Source:              record.Source,
		ReasoningEffort:     record.ReasoningEffort,
		ServiceTier:         record.EffectiveServiceTier,
		Stream:              record.Stream,
		Failed:              record.Failed,
		LatencyMs:           record.Latency.Milliseconds(),
		TTFTMs:              record.TTFT.Milliseconds(),
		InputTokens:         detail.InputTokens,
		OutputTokens:        detail.OutputTokens,
		ReasoningTokens:     detail.ReasoningTokens,
		CachedTokens:        detail.CachedTokens,
		CacheReadTokens:     detail.CacheReadTokens,
		CacheCreationTokens: detail.CacheCreationTokens,
		TotalTokens:         detail.TotalTokens,
	}
}

func clickhouseTime(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(clickhouseTimeLayout)
}

func maskSecret(secret string) string {
	if len(secret) <= 12 {
		return "***"
	}
	return secret[:4] + "..." + secret[len(secret)-4:]
}
