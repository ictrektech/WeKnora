package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVNoteScheduleBoundariesAndDST(t *testing.T) {
	for _, tc := range []struct {
		name, zone, now, next string
		start, end            int
		allowed               bool
	}{
		{"start", "Asia/Shanghai", "2026-11-13T16:00:00Z", "", 0, 6, true},
		{"end", "Asia/Shanghai", "2026-11-13T22:00:00Z", "2026-11-14T16:00:00Z", 0, 6, false},
		{"overnight", "Asia/Shanghai", "2026-11-13T15:00:00Z", "", 22, 6, true},
		{"daytime", "Asia/Shanghai", "2026-11-13T02:00:00Z", "2026-11-13T14:00:00Z", 22, 6, false},
		{"spring_skipped_hour", "America/Los_Angeles", "2026-03-08T09:59:00Z", "2026-03-09T09:00:00Z", 2, 3, false},
		{"fall_repeated_hour", "America/Los_Angeles", "2026-11-01T09:30:00Z", "", 1, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.now)
			require.NoError(t, err)
			allowed, next, err := (vnoteSchedule{Mode: "night", IntervalMinutes: 15, TimeZone: tc.zone, NightStart: tc.start, NightEnd: tc.end}).window(now)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
			if tc.next != "" {
				require.Equal(t, tc.next, next.UTC().Format(time.RFC3339))
			}
		})
	}
}

func TestVNoteScheduleInvalidAndManualNeverAllowAutomaticWork(t *testing.T) {
	p := vnoteSchedule{Mode: "manual", IntervalMinutes: 15, TimeZone: "UTC", NightStart: 0, NightEnd: 6}
	allowed, _, err := p.window(time.Now())
	require.NoError(t, err)
	require.False(t, allowed)
	for _, mutate := range []func(*vnoteSchedule){func(p *vnoteSchedule) { p.Mode = "future" }, func(p *vnoteSchedule) { p.TimeZone = "Invalid/Zone" }, func(p *vnoteSchedule) { p.IntervalMinutes = 5 }, func(p *vnoteSchedule) { p.NightStart = p.NightEnd }} {
		broken := p
		mutate(&broken)
		allowed, _, err := broken.window(time.Now())
		require.Error(t, err)
		require.False(t, allowed)
	}
}
