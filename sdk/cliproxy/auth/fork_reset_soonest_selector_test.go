package auth

import (
	"context"
	"strconv"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
)

func resetSoonestAuth(id, priority string, signals map[string]string) *Auth {
	return &Auth{
		ID:         id,
		Provider:   "codex",
		Status:     StatusActive,
		Attributes: map[string]string{"priority": priority},
		Quota:      QuotaState{Signals: signals, ObservedAt: time.Now()},
	}
}

func unixIn(d time.Duration) string { return strconv.FormatInt(time.Now().Add(d).Unix(), 10) }

func TestResetSoonestPicksEarliestWeeklyReset(t *testing.T) {
	t.Parallel()
	later := resetSoonestAuth("a-later", "0", map[string]string{
		"X-Codex-Secondary-Window-Minutes": "10080",
		"X-Codex-Secondary-Reset-At":       unixIn(5 * 24 * time.Hour),
	})
	sooner := resetSoonestAuth("b-sooner", "0", map[string]string{
		// Primary is the 5h window; only the weekly slot counts.
		"X-Codex-Primary-Window-Minutes":   "300",
		"X-Codex-Primary-Reset-At":         unixIn(time.Hour),
		"X-Codex-Secondary-Window-Minutes": "10080",
		"X-Codex-Secondary-Reset-At":       unixIn(2 * 24 * time.Hour),
	})
	unknown := resetSoonestAuth("c-unknown", "0", nil)
	got, err := (&ResetSoonestSelector{}).Pick(context.Background(), "codex", "gpt", cliproxyexecutor.Options{}, []*Auth{later, unknown, sooner})
	if err != nil || got == nil || got.ID != sooner.ID {
		t.Fatalf("Pick = %v (err=%v), want %q", got, err, sooner.ID)
	}
}

func TestResetSoonestReadsClaudeAndRelativeCodexResets(t *testing.T) {
	t.Parallel()
	claude := resetSoonestAuth("a-claude", "0", map[string]string{
		"Anthropic-Ratelimit-Unified-7d-Reset": unixIn(3 * 24 * time.Hour),
	})
	claude.Provider = "claude"
	codexRelative := resetSoonestAuth("b-codex", "0", map[string]string{
		"X-Codex-Primary-Window-Minutes":      "10080",
		"X-Codex-Primary-Reset-After-Seconds": "3600.5",
	})
	if at, ok := weeklyResetAt(claude, time.Now()); !ok || at.Before(time.Now().Add(71*time.Hour)) {
		t.Fatalf("claude weekly reset = %v, %v", at, ok)
	}
	got, err := (&ResetSoonestSelector{}).Pick(context.Background(), "mixed", "m", cliproxyexecutor.Options{}, []*Auth{claude, codexRelative})
	if err != nil || got == nil || got.ID != codexRelative.ID {
		t.Fatalf("Pick = %v (err=%v), want %q", got, err, codexRelative.ID)
	}
}

func TestResetSoonestHonorsPriorityAndPastResets(t *testing.T) {
	t.Parallel()
	lowSoon := resetSoonestAuth("a-low", "0", map[string]string{
		"Anthropic-Ratelimit-Unified-7d-Reset": unixIn(time.Hour),
	})
	highLater := resetSoonestAuth("b-high", "1", map[string]string{
		"Anthropic-Ratelimit-Unified-7d-Reset": unixIn(6 * 24 * time.Hour),
	})
	got, err := (&ResetSoonestSelector{}).Pick(context.Background(), "claude", "m", cliproxyexecutor.Options{}, []*Auth{lowSoon, highLater})
	if err != nil || got == nil || got.ID != highLater.ID {
		t.Fatalf("priority: Pick = %v (err=%v), want %q", got, err, highLater.ID)
	}

	// A reset in the past means the window already renewed: unknown, so round-robin.
	expiredA := resetSoonestAuth("a", "0", map[string]string{"Anthropic-Ratelimit-Unified-7d-Reset": unixIn(-time.Hour)})
	expiredB := resetSoonestAuth("b", "0", nil)
	selector := &ResetSoonestSelector{}
	seen := map[string]bool{}
	for range 2 {
		got, err := selector.Pick(context.Background(), "claude", "m", cliproxyexecutor.Options{}, []*Auth{expiredA, expiredB})
		if err != nil || got == nil {
			t.Fatalf("round-robin Pick = %v (err=%v)", got, err)
		}
		seen[got.ID] = true
	}
	if len(seen) != 2 {
		t.Fatalf("expected round-robin over unknown resets, saw %v", seen)
	}
}
