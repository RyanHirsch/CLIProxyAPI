package main

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

type pluginConfig struct {
	Preferences []preference `yaml:"preferences"`
	ResetFirst  bool         `yaml:"reset-first"`
}

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  struct {
		Scheduler   bool `json:"scheduler"`
		UsagePlugin bool `json:"usage_plugin"`
	} `json:"capabilities"`
}

var (
	currentConfig atomic.Pointer[pluginConfig]
	resets        = newResetTracker(time.Now)
)

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodSchedulerPick:
		return pickAuth(request)
	case pluginabi.MethodUsageHandle:
		return observeUsage(request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
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
	cfg := &pluginConfig{}
	if errDecode := yaml.Unmarshal(req.ConfigYAML, cfg); errDecode != nil {
		return errDecode
	}
	currentConfig.Store(cfg)
	return nil
}

func pluginRegistration() registration {
	reg := registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             "account-routing",
			Version:          "0.4.0",
			Author:           "RyanHirsch",
			GitHubRepository: "https://github.com/RyanHirsch/CLIProxyAPI",
			ConfigFields: []pluginapi.ConfigField{{
				Name:        "preferences",
				Type:        pluginapi.ConfigFieldTypeArray,
				Default:     []any{},
				Description: "Ordered rules, first match wins. Each rule is an object with client-key (a client API key) and auth-ids (credential IDs to prefer, in order). Requests without an available preferred credential fall through to reset-first or normal routing.",
			}, {
				Name:        "reset-first",
				Type:        pluginapi.ConfigFieldTypeBoolean,
				Default:     false,
				Description: "Route requests without an available preferred credential to the credential whose quota resets soonest: 5-hour window first, then weekly-only, then credentials not yet seen, then credentials without quota windows.",
			}},
		},
	}
	reg.Capabilities.Scheduler = true
	reg.Capabilities.UsagePlugin = true
	return reg
}

func pickAuth(raw []byte) ([]byte, error) {
	var req pluginapi.SchedulerPickRequest
	if errUnmarshal := json.Unmarshal(raw, &req); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	cfg := currentConfig.Load()
	if cfg == nil {
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}
	availableAuthIDs := make([]string, 0, len(req.Candidates))
	for _, candidate := range req.Candidates {
		availableAuthIDs = append(availableAuthIDs, candidate.ID)
	}
	authID, ok := preferredAuthID(cfg.Preferences, req.Options.Headers, availableAuthIDs)
	if !ok && cfg.ResetFirst {
		authID, ok = resets.soonestReset(availableAuthIDs)
	}
	return okEnvelope(pluginapi.SchedulerPickResponse{AuthID: authID, Handled: ok})
}

func observeUsage(raw []byte) ([]byte, error) {
	var record struct {
		AuthID          string
		ResponseHeaders http.Header
	}
	if errUnmarshal := json.Unmarshal(raw, &record); errUnmarshal != nil {
		return nil, errUnmarshal
	}
	resets.observe(record.AuthID, record.ResponseHeaders)
	return okEnvelope(struct{}{})
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
