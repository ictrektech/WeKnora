package service

import (
	"errors"
	"time"
	_ "time/tzdata"
)

type vnoteSchedule struct {
	Mode            string `json:"mode"`
	IntervalMinutes int    `json:"intervalMinutes"`
	TimeZone        string `json:"timeZone"`
	NightStart      int    `json:"nightStart"`
	NightEnd        int    `json:"nightEnd"`
}

type vnoteSyncPlan struct {
	Protocol      int            `json:"protocol"`
	Subject       string         `json:"subject"`
	WorkspaceID   string         `json:"workspaceId"`
	RootID        string         `json:"rootId"`
	ConfigVersion int64          `json:"configVersion"`
	Schedule      *vnoteSchedule `json:"schedule"`
	Paused        bool           `json:"paused"`
	RootName      *string        `json:"rootName"`
	RootMissing   bool           `json:"rootMissing"`
}

// UTC minute stepping handles skipped and repeated local hours during DST.
// Invalid/unknown plans always prevent unattended indexing.
func (p vnoteSchedule) window(now time.Time) (bool, time.Time, error) {
	if p.IntervalMinutes < 15 || p.IntervalMinutes > 1440 || p.NightStart < 0 || p.NightStart > 23 || p.NightEnd < 0 || p.NightEnd > 23 || p.NightStart == p.NightEnd {
		return false, time.Time{}, errors.New("invalid V-Note schedule")
	}
	zone, err := time.LoadLocation(p.TimeZone)
	if err != nil {
		return false, time.Time{}, errors.New("invalid V-Note time zone")
	}
	switch p.Mode {
	case "manual":
		return false, now.Add(15 * time.Minute), nil
	case "interval":
		return true, now, nil
	case "night":
		contains := func(t time.Time) bool {
			hour := t.In(zone).Hour()
			if p.NightStart < p.NightEnd {
				return hour >= p.NightStart && hour < p.NightEnd
			}
			return hour >= p.NightStart || hour < p.NightEnd
		}
		if contains(now) {
			return true, now, nil
		}
		candidate := now.UTC().Truncate(time.Minute).Add(time.Minute)
		for i := 0; i < 49*60; i++ {
			if contains(candidate) {
				return false, candidate, nil
			}
			candidate = candidate.Add(time.Minute)
		}
	}
	return false, time.Time{}, errors.New("unsupported V-Note schedule")
}
