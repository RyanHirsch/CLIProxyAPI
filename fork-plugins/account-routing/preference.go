package main

import (
	"slices"
	"strings"
)

type preference struct {
	ClientKey string   `yaml:"client-key"`
	AuthIDs   []string `yaml:"auth-ids"`
}

func preferredAuthID(rules []preference, headers map[string][]string, availableAuthIDs []string) (string, bool) {
	presented := presentedClientKeys(headers)
	for _, rule := range rules {
		if rule.ClientKey == "" || !slices.Contains(presented, rule.ClientKey) {
			continue
		}
		for _, authID := range rule.AuthIDs {
			if slices.Contains(availableAuthIDs, authID) {
				return authID, true
			}
		}
		return "", false
	}
	return "", false
}

func presentedClientKeys(headers map[string][]string) []string {
	var keys []string
	for name, values := range headers {
		switch strings.ToLower(name) {
		case "authorization":
			for _, value := range values {
				keys = append(keys, bearerToken(value))
			}
		case "x-api-key", "x-goog-api-key":
			keys = append(keys, values...)
		}
	}
	return keys
}

func bearerToken(header string) string {
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return header
	}
	return strings.TrimSpace(token)
}
