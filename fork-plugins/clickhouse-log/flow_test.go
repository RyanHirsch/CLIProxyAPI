package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MoeForge/moego/json"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestStreamedExchangeIsWrittenAsOnePromptRowAndOneUsageRow(t *testing.T) {
	targetURL, inserts := clickhouseUnderTest(t)
	call(t, pluginabi.MethodPluginRegister, map[string]any{
		"config_yaml": []byte("url: " + targetURL + "\nusername: default\npassword: secret\n"),
	})

	call(t, pluginabi.MethodRequestInterceptBefore, pluginapi.RequestInterceptRequest{RequestID: "req-1", Body: []byte(`{"prompt":"hi"}`)})
	call(t, pluginabi.MethodResponseInterceptStreamChunk, pluginapi.StreamChunkInterceptRequest{RequestID: "req-1", ChunkIndex: pluginapi.StreamChunkHeaderInitIndex, Body: []byte("ignored")})
	call(t, pluginabi.MethodResponseInterceptStreamChunk, pluginapi.StreamChunkInterceptRequest{RequestID: "req-1", ChunkIndex: 0, Body: []byte(`{"delta":"hel"}`)})
	call(t, pluginabi.MethodResponseInterceptStreamChunk, pluginapi.StreamChunkInterceptRequest{RequestID: "req-1", ChunkIndex: 1, Body: []byte(`{"delta":"lo"}`)})
	call(t, pluginabi.MethodUsageHandle, pluginapi.UsageRecord{
		RequestID: "req-1", Model: "gpt-6.2", APIKey: "sk-client-1234567890",
		Latency: 1500 * time.Millisecond, Detail: pluginapi.UsageDetail{InputTokens: 10, OutputTokens: 2},
	})
	call(t, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{
		RequestID: "req-1", Model: "gpt-6.2", Stream: true, Outcome: pluginapi.RequestCompletionSucceeded, StatusCode: 200,
	})
	call(t, pluginabi.MethodResponseInterceptAfter, pluginapi.ResponseInterceptRequest{RequestID: "model-list", Body: []byte(`{"data":[]}`)})
	call(t, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: "model-list", Outcome: pluginapi.RequestCompletionSucceeded})
	shutdown()

	var prompt promptRow
	decodeOnlyRow(t, inserts.rows("prompts"), &prompt)
	if prompt.RequestBody != `{"prompt":"hi"}` || prompt.ResponseBody != "{\"delta\":\"hel\"}\n{\"delta\":\"lo\"}\n" || prompt.Outcome != "succeeded" {
		t.Fatalf("prompt row = %+v", prompt)
	}
	var usage usageRow
	decodeOnlyRow(t, inserts.rows("usage"), &usage)
	if usage.InputTokens != 10 || usage.OutputTokens != 2 || usage.LatencyMs != 1500 || usage.ClientKey != "sk-c...7890" {
		t.Fatalf("usage row = %+v", usage)
	}
	if !strings.Contains(inserts.schema(), "INTERVAL 90 DAY") {
		t.Fatalf("schema missing 90 day TTL: %s", inserts.schema())
	}
}

func clickhouseUnderTest(t *testing.T) (string, *recordedQueries) {
	recorded := &recordedQueries{byTable: map[string][]string{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != "default" || pass != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		recorded.add(r.URL.Query().Get("query"), string(body))
	}))
	t.Cleanup(server.Close)
	return server.URL, recorded
}

type recordedQueries struct {
	mu      sync.Mutex
	ddl     strings.Builder
	byTable map[string][]string
}

func (r *recordedQueries) add(query, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !strings.HasPrefix(query, "INSERT INTO cliproxy.") {
		r.ddl.WriteString(query)
		return
	}
	table := strings.Fields(strings.TrimPrefix(query, "INSERT INTO cliproxy."))[0]
	r.byTable[table] = append(r.byTable[table], strings.Split(strings.TrimSpace(body), "\n")...)
}

func (r *recordedQueries) rows(table string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byTable[table]
}

func (r *recordedQueries) schema() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ddl.String()
}

func call(t *testing.T, method string, payload any) {
	t.Helper()
	raw, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		t.Fatal(errMarshal)
	}
	resp, errHandle := handleMethod(method, raw)
	if errHandle != nil {
		t.Fatalf("%s: %v", method, errHandle)
	}
	var env envelope
	if errUnmarshal := json.Unmarshal(resp, &env); errUnmarshal != nil || !env.OK {
		t.Fatalf("%s: response %s", method, resp)
	}
}

func decodeOnlyRow(t *testing.T, rows []string, into any) {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1: %v", len(rows), rows)
	}
	if errUnmarshal := json.Unmarshal([]byte(rows[0]), into); errUnmarshal != nil {
		t.Fatal(errUnmarshal)
	}
}
