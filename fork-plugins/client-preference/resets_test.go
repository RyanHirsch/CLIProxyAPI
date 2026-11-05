package main

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

var testNow = time.Unix(1_800_000_000, 0)

func epoch(offset time.Duration) string {
	return strconv.FormatInt(testNow.Add(offset).Unix(), 10)
}

func codexHeaders(primaryReset, primaryMinutes, secondaryReset, secondaryMinutes string) http.Header {
	return http.Header{
		"X-Codex-Primary-Reset-At":         {primaryReset},
		"X-Codex-Primary-Window-Minutes":   {primaryMinutes},
		"X-Codex-Secondary-Reset-At":       {secondaryReset},
		"X-Codex-Secondary-Window-Minutes": {secondaryMinutes},
	}
}

var creditsOnly = codexHeaders("0", "0", "0", "0")

func TestSoonestReset(t *testing.T) {
	tests := []struct {
		name       string
		observed   map[string]http.Header
		candidates []string
		want       string
		wantOK     bool
	}{
		{
			name: "windowed account before credits-only account",
			observed: map[string]http.Header{
				"credits": creditsOnly,
				"plus":    codexHeaders(epoch(2*time.Hour), "300", epoch(72*time.Hour), "10080"),
			},
			want: "plus", wantOK: true,
		},
		{
			name: "sooner 5-hour reset wins",
			observed: map[string]http.Header{
				"a": codexHeaders(epoch(3*time.Hour), "300", epoch(24*time.Hour), "10080"),
				"b": codexHeaders(epoch(1*time.Hour), "300", epoch(100*time.Hour), "10080"),
			},
			want: "b", wantOK: true,
		},
		{
			name: "weekly reset breaks a 5-hour tie",
			observed: map[string]http.Header{
				"a": codexHeaders(epoch(1*time.Hour), "300", epoch(90*time.Hour), "10080"),
				"b": codexHeaders(epoch(1*time.Hour), "300", epoch(30*time.Hour), "10080"),
			},
			want: "b", wantOK: true,
		},
		{
			name: "active 5-hour window before a sooner weekly-only reset",
			observed: map[string]http.Header{
				"weekly-only": codexHeaders(epoch(30*time.Minute), "10080", "0", "0"),
				"five-hour":   codexHeaders(epoch(4*time.Hour), "300", epoch(100*time.Hour), "10080"),
			},
			want: "five-hour", wantOK: true,
		},
		{
			name: "expired windows still rank before credits-only",
			observed: map[string]http.Header{
				"credits": creditsOnly,
				"expired": codexHeaders(epoch(-time.Hour), "300", epoch(-time.Minute), "10080"),
			},
			want: "expired", wantOK: true,
		},
		{
			name:       "unobserved account before credits-only account",
			observed:   map[string]http.Header{"credits": creditsOnly},
			candidates: []string{"credits", "unobserved"},
			want:       "unobserved", wantOK: true,
		},
		{
			name: "claude 5h and 7d resets",
			observed: map[string]http.Header{
				"a": {"Anthropic-Ratelimit-Unified-5h-Reset": {epoch(2 * time.Hour)}, "Anthropic-Ratelimit-Unified-7d-Reset": {epoch(48 * time.Hour)}},
				"b": {"Anthropic-Ratelimit-Unified-5h-Reset": {epoch(1 * time.Hour)}, "Anthropic-Ratelimit-Unified-7d-Reset": {epoch(96 * time.Hour)}},
			},
			want: "b", wantOK: true,
		},
		{
			name: "reset after seconds when reset-at is missing",
			observed: map[string]http.Header{
				"a": {"X-Codex-Primary-Reset-After-Seconds": {"7200"}, "X-Codex-Primary-Window-Minutes": {"300"}},
				"b": {"X-Codex-Primary-Reset-After-Seconds": {"600"}, "X-Codex-Primary-Window-Minutes": {"300"}},
			},
			want: "b", wantOK: true,
		},
		{
			name:       "nothing observed defers to normal routing",
			observed:   map[string]http.Header{"other": {"Content-Type": {"application/json"}}},
			candidates: []string{"unobserved", "other"},
			want:       "", wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := newResetTracker(func() time.Time { return testNow })
			candidates := tt.candidates
			for authID, headers := range tt.observed {
				tracker.observe(authID, headers)
				if tt.candidates == nil {
					candidates = append(candidates, authID)
				}
			}
			got, ok := tracker.soonestReset(candidates)
			if ok != tt.wantOK || (ok && got != tt.want) {
				t.Fatalf("soonestReset() = %q, %v; want %q, %v", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestObserveKeepsWindowsWhenResponseHasNoQuotaHeaders(t *testing.T) {
	tracker := newResetTracker(func() time.Time { return testNow })
	tracker.observe("plus", codexHeaders(epoch(time.Hour), "300", epoch(48*time.Hour), "10080"))
	tracker.observe("plus", http.Header{"Content-Type": {"text/event-stream"}})
	tracker.observe("credits", creditsOnly)
	if got, _ := tracker.soonestReset([]string{"credits", "plus"}); got != "plus" {
		t.Fatalf("soonestReset() = %q; want plus", got)
	}
}
