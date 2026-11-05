package main

import (
	"encoding/json"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

type pluginConfig struct {
	Preferences []preference `yaml:"preferences"`
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
		Scheduler bool `json:"scheduler"`
	} `json:"capabilities"`
}

var currentConfig atomic.Pointer[pluginConfig]

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		if errConfigure := configure(request); errConfigure != nil {
			return nil, errConfigure
		}
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodSchedulerPick:
		return pickAuth(request)
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
			Name:             "client-preference",
			Version:          "0.1.0",
			Author:           "RyanHirsch",
			GitHubRepository: "https://github.com/RyanHirsch/CLIProxyAPI",
		},
	}
	reg.Capabilities.Scheduler = true
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
	if !ok {
		return okEnvelope(pluginapi.SchedulerPickResponse{Handled: false})
	}
	return okEnvelope(pluginapi.SchedulerPickResponse{AuthID: authID, Handled: true})
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
