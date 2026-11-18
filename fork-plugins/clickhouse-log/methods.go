package main

import (
	"sync"

	"github.com/MoeForge/moego/json"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type capabilities struct {
	RequestInterceptor     bool `json:"request_interceptor"`
	OmitBodyAfterAuth      bool `json:"request_interceptor_omit_body_after"`
	RequestLifecyclePlugin bool `json:"request_lifecycle_plugin"`
	ResponseInterceptor    bool `json:"response_interceptor"`
	StreamChunkInterceptor bool `json:"response_stream_interceptor"`
	UsagePlugin            bool `json:"usage_plugin"`
}

type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  capabilities       `json:"capabilities"`
}

var (
	exchanges     = newExchangeLog()
	queue         = newRowQueue()
	closeQueue    sync.Once
	unchangedBody = struct{}{}
)

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodPluginShutdown:
		shutdown()
		return okEnvelope(unchangedBody)
	case pluginabi.MethodRequestInterceptBefore:
		return observe(request, func(req pluginapi.RequestInterceptRequest) {
			exchanges.recordRequest(req.RequestID, req.Body)
		})
	case pluginabi.MethodRequestInterceptAfter:
		return okEnvelope(unchangedBody)
	case pluginabi.MethodResponseInterceptAfter:
		return observe(request, func(req pluginapi.ResponseInterceptRequest) {
			exchanges.appendResponse(req.RequestID, req.Body)
		})
	case pluginabi.MethodResponseInterceptStreamChunk:
		return observe(request, func(req pluginapi.StreamChunkInterceptRequest) {
			if req.ChunkIndex >= 0 {
				exchanges.appendResponse(req.RequestID, append(req.Body, '\n'))
			}
		})
	case pluginabi.MethodRequestComplete:
		return observe(request, func(completion pluginapi.RequestCompletion) {
			if captured := exchanges.finish(completion.RequestID); len(captured.requestBody) > 0 {
				queue.enqueue("prompts", newPromptRow(completion, captured))
			}
		})
	case pluginabi.MethodUsageHandle:
		return observe(request, func(record pluginapi.UsageRecord) {
			queue.enqueue("usage", newUsageRow(record))
		})
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func observe[T any](raw []byte, handle func(T)) ([]byte, error) {
	var payload T
	if errUnmarshal := json.Unmarshal(raw, &payload); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	handle(payload)
	return okEnvelope(unchangedBody)
}

func shutdown() {
	closeQueue.Do(queue.close)
}

func configure(raw []byte) error {
	var req struct {
		ConfigYAML []byte `json:"config_yaml"`
	}
	if len(raw) > 0 {
		if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
			return errUnmarshal
		}
	}
	target := &clickhouseTarget{Database: "cliproxy"}
	if errDecode := yaml.Unmarshal(req.ConfigYAML, target); errDecode != nil {
		return errDecode
	}
	queue.target.Store(target)
	return nil
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "clickhouse-log",
			Version:          "0.1.0",
			Author:           "RyanHirsch",
			GitHubRepository: "https://github.com/RyanHirsch/CLIProxyAPI",
			ConfigFields: []pluginapi.ConfigField{{
				Name:        "url",
				Type:        pluginapi.ConfigFieldTypeString,
				Default:     "",
				Description: "ClickHouse HTTP endpoint, e.g. http://clickhouse:8123. Logging is off while empty.",
			}, {
				Name:        "database",
				Type:        pluginapi.ConfigFieldTypeString,
				Default:     "cliproxy",
				Description: "Database holding the prompts and usage tables. Created on first write.",
			}, {
				Name:        "username",
				Type:        pluginapi.ConfigFieldTypeString,
				Default:     "",
				Description: "ClickHouse user.",
			}, {
				Name:        "password",
				Type:        pluginapi.ConfigFieldTypeString,
				Default:     "",
				Description: "ClickHouse password.",
			}},
		},
		Capabilities: capabilities{
			RequestInterceptor:     true,
			OmitBodyAfterAuth:      true,
			RequestLifecyclePlugin: true,
			ResponseInterceptor:    true,
			StreamChunkInterceptor: true,
			UsagePlugin:            true,
		},
	}
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}
