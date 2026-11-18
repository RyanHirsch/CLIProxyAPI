package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/MoeForge/moego/json"
	log "github.com/sirupsen/logrus"
)

type clickhouseTarget struct {
	URL      string `yaml:"url"`
	Database string `yaml:"database"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

const retentionDays = 90

var errUnreachable = errors.New("clickhouse unreachable")

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS %[1]s.prompts (
		request_id String,
		trace_id String,
		started_at DateTime64(3, 'UTC'),
		completed_at DateTime64(3, 'UTC'),
		source_format LowCardinality(String),
		model LowCardinality(String),
		requested_model LowCardinality(String),
		stream Bool,
		outcome LowCardinality(String),
		status_code UInt16,
		error String,
		request_body String CODEC(ZSTD(3)),
		response_body String CODEC(ZSTD(3))
	) ENGINE = MergeTree
	PARTITION BY toYYYYMM(started_at)
	ORDER BY (started_at, request_id)
	TTL toDateTime(started_at) + INTERVAL %[2]d DAY`,
	`CREATE TABLE IF NOT EXISTS %[1]s.usage (
		request_id String,
		trace_id String,
		requested_at DateTime64(3, 'UTC'),
		provider LowCardinality(String),
		executor_type LowCardinality(String),
		model LowCardinality(String),
		response_model LowCardinality(String),
		alias LowCardinality(String),
		client_key LowCardinality(String),
		auth_id LowCardinality(String),
		auth_type LowCardinality(String),
		source LowCardinality(String),
		reasoning_effort LowCardinality(String),
		service_tier LowCardinality(String),
		stream Bool,
		failed Bool,
		latency_ms Int64,
		ttft_ms Int64,
		input_tokens Int64,
		output_tokens Int64,
		reasoning_tokens Int64,
		cached_tokens Int64,
		cache_read_tokens Int64,
		cache_creation_tokens Int64,
		total_tokens Int64
	) ENGINE = MergeTree
	PARTITION BY toYYYYMM(requested_at)
	ORDER BY (requested_at, request_id)
	TTL toDateTime(requested_at) + INTERVAL %[2]d DAY`,
	`CREATE VIEW IF NOT EXISTS %[1]s.exchanges AS
	SELECT p.*, u.request_id AS attempt_id, u.* EXCEPT (request_id, trace_id, model, stream)
	FROM %[1]s.prompts AS p
	LEFT JOIN %[1]s.usage AS u
		ON u.trace_id = p.trace_id
		AND u.requested_at >= p.started_at
		AND u.requested_at <= p.completed_at`,
}

func (t clickhouseTarget) ensureSchema(ctx context.Context) error {
	if errCreate := t.exec(ctx, "CREATE DATABASE IF NOT EXISTS "+t.Database, nil); errCreate != nil {
		return errCreate
	}
	for _, statement := range schemaStatements {
		if errCreate := t.exec(ctx, fmt.Sprintf(statement, t.Database, retentionDays), nil); errCreate != nil {
			return errCreate
		}
	}
	return nil
}

func (t clickhouseTarget) insert(ctx context.Context, table string, rows []any) error {
	var body bytes.Buffer
	for _, row := range rows {
		line, errMarshal := json.Marshal(row)
		if errMarshal != nil {
			return errMarshal
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	return t.exec(ctx, fmt.Sprintf("INSERT INTO %s.%s FORMAT JSONEachRow", t.Database, table), &body)
}

func (t clickhouseTarget) exec(ctx context.Context, query string, body io.Reader) error {
	endpoint := strings.TrimRight(t.URL, "/") + "/?query=" + url.QueryEscape(query)
	req, errRequest := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, body)
	if errRequest != nil {
		return errRequest
	}
	if t.Username != "" {
		req.SetBasicAuth(t.Username, t.Password)
	}
	resp, errDo := http.DefaultClient.Do(req)
	if errDo != nil {
		return fmt.Errorf("%w: %w", errUnreachable, errDo)
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.Errorf("clickhouse-log: close response body: %v", errClose)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return fmt.Errorf("clickhouse %s: %s", resp.Status, strings.TrimSpace(string(detail)))
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
