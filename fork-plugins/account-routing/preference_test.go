package main

import "testing"

func TestPreferredAuthID(t *testing.T) {
	rules := []preference{
		{ClientKey: "m4-key", AuthIDs: []string{"codex-plus.json", "claude-a.json"}},
		{ClientKey: "mysten-key", AuthIDs: []string{"codex-business.json"}},
	}
	allAvailable := []string{"codex-business.json", "codex-plus.json", "claude-a.json"}

	tests := []struct {
		name      string
		headers   map[string][]string
		available []string
		want      string
		wantOK    bool
	}{
		{
			name:      "bearer key picks its preferred account",
			headers:   map[string][]string{"Authorization": {"Bearer m4-key"}},
			available: allAvailable,
			want:      "codex-plus.json",
			wantOK:    true,
		},
		{
			name:      "anthropic x-api-key header is matched",
			headers:   map[string][]string{"X-Api-Key": {"mysten-key"}},
			available: allAvailable,
			want:      "codex-business.json",
			wantOK:    true,
		},
		{
			name:      "google api key header is matched",
			headers:   map[string][]string{"X-Goog-Api-Key": {"mysten-key"}},
			available: allAvailable,
			want:      "codex-business.json",
			wantOK:    true,
		},
		{
			name:      "header names and bearer scheme are case-insensitive",
			headers:   map[string][]string{"authorization": {"bearer m4-key"}},
			available: allAvailable,
			want:      "codex-plus.json",
			wantOK:    true,
		},
		{
			name:      "later preference is used when earlier one is unavailable",
			headers:   map[string][]string{"Authorization": {"Bearer m4-key"}},
			available: []string{"codex-business.json", "claude-a.json"},
			want:      "claude-a.json",
			wantOK:    true,
		},
		{
			name:      "no preferred account available falls through",
			headers:   map[string][]string{"Authorization": {"Bearer mysten-key"}},
			available: []string{"codex-plus.json"},
			wantOK:    false,
		},
		{
			name:      "unknown client key falls through",
			headers:   map[string][]string{"Authorization": {"Bearer someone-else"}},
			available: allAvailable,
			wantOK:    false,
		},
		{
			name:      "missing credentials fall through",
			headers:   map[string][]string{"Content-Type": {"application/json"}},
			available: allAvailable,
			wantOK:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := preferredAuthID(rules, tt.headers, tt.available)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("preferredAuthID() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
