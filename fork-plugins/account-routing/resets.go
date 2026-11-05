package main

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const shortWindowLimit = 24 * time.Hour

type quotaWindows struct {
	shortReset  time.Time
	weeklyReset time.Time
	windowed    bool
}

type resetTracker struct {
	mu     sync.RWMutex
	byAuth map[string]quotaWindows
	now    func() time.Time
}

func newResetTracker(now func() time.Time) *resetTracker {
	return &resetTracker{byAuth: make(map[string]quotaWindows), now: now}
}

func (t *resetTracker) observe(authID string, headers http.Header) {
	if authID == "" {
		return
	}
	windows, ok := quotaWindowsFromHeaders(headers, t.now())
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	windows.windowed = windows.windowed || t.byAuth[authID].windowed
	t.byAuth[authID] = windows
}

// soonestReset ranks candidates whose quota resets soonest first and reports
// false when no candidate has been observed yet.
func (t *resetTracker) soonestReset(authIDs []string) (string, bool) {
	now := t.now()
	t.mu.RLock()
	defer t.mu.RUnlock()
	best, bestRank, anyObserved := "", resetRank{tier: tierUnobserved}, false
	for _, authID := range authIDs {
		windows, observed := t.byAuth[authID]
		anyObserved = anyObserved || observed
		rank := rankWindows(windows, observed, now)
		if best == "" || rank.before(bestRank) {
			best, bestRank = authID, rank
		}
	}
	return best, anyObserved
}

const (
	tierShortWindow = iota
	tierWeeklyWindow
	tierUnobserved
	tierWithoutWindows
)

type resetRank struct {
	tier   int
	first  time.Time
	second time.Time
}

func (r resetRank) before(other resetRank) bool {
	if r.tier != other.tier {
		return r.tier < other.tier
	}
	if !r.first.Equal(other.first) {
		return r.first.Before(other.first)
	}
	return r.second.Before(other.second)
}

func rankWindows(windows quotaWindows, observed bool, now time.Time) resetRank {
	shortActive := windows.shortReset.After(now)
	weeklyActive := windows.weeklyReset.After(now)
	switch {
	case shortActive:
		return resetRank{tier: tierShortWindow, first: windows.shortReset, second: windows.weeklyReset}
	case weeklyActive:
		return resetRank{tier: tierWeeklyWindow, first: windows.weeklyReset}
	case !observed || windows.windowed:
		return resetRank{tier: tierUnobserved}
	default:
		return resetRank{tier: tierWithoutWindows}
	}
}

func quotaWindowsFromHeaders(headers http.Header, now time.Time) (quotaWindows, bool) {
	var windows quotaWindows
	observed := false
	for name := range headers {
		lower := strings.ToLower(name)
		if strings.HasPrefix(lower, "x-codex-primary-") || strings.HasPrefix(lower, "x-codex-secondary-") ||
			strings.HasPrefix(lower, "anthropic-ratelimit-unified-") {
			observed = true
			break
		}
	}
	if !observed {
		return windows, false
	}
	for _, prefix := range []string{"X-Codex-Primary-", "X-Codex-Secondary-"} {
		reset := codexReset(headers, prefix, now)
		if reset.IsZero() {
			continue
		}
		minutes := headerInt(headers, prefix+"Window-Minutes")
		isShort := prefix == "X-Codex-Primary-"
		if minutes > 0 {
			isShort = time.Duration(minutes)*time.Minute <= shortWindowLimit
		}
		windows.setReset(isShort, reset)
	}
	windows.setReset(true, epochTime(headerInt(headers, "Anthropic-Ratelimit-Unified-5h-Reset")))
	windows.setReset(false, epochTime(headerInt(headers, "Anthropic-Ratelimit-Unified-7d-Reset")))
	return windows, true
}

func (w *quotaWindows) setReset(isShort bool, reset time.Time) {
	if reset.IsZero() {
		return
	}
	w.windowed = true
	if isShort {
		w.shortReset = reset
	} else {
		w.weeklyReset = reset
	}
}

func codexReset(headers http.Header, prefix string, now time.Time) time.Time {
	if resetAt := epochTime(headerInt(headers, prefix+"Reset-At")); !resetAt.IsZero() {
		return resetAt
	}
	if seconds := headerInt(headers, prefix+"Reset-After-Seconds"); seconds > 0 {
		return now.Add(time.Duration(seconds) * time.Second)
	}
	return time.Time{}
}

func epochTime(seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	return time.Unix(seconds, 0)
}

func headerInt(headers http.Header, name string) int64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(headers.Get(name)), 64)
	return int64(value)
}
