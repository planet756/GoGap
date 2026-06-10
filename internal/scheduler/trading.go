package scheduler

import "time"

// ChinaStandardTime is the CST (UTC+8) timezone used for all trading‑hour
// and NAV date calculations.
var ChinaStandardTime = time.FixedZone("CST", 8*60*60)

// chinaHolidays lists known China mainland exchange holiday dates (2025‑2027).
// Only non‑weekend dates are listed; weekends are always excluded by isTradingHours.
var chinaHolidays = func() map[string]bool {
	dates := []string{
		// 2025
		"2025-01-01", "2025-01-28", "2025-01-29", "2025-01-30", "2025-01-31",
		"2025-02-03", "2025-02-04", "2025-04-04", "2025-05-01", "2025-05-02",
		"2025-05-05", "2025-05-30", "2025-10-01", "2025-10-02", "2025-10-03",
		"2025-10-06", "2025-10-07", "2025-10-08",
		// 2026
		"2026-01-01", "2026-01-02", "2026-02-16", "2026-02-17", "2026-02-18",
		"2026-02-19", "2026-02-20", "2026-04-06", "2026-05-01", "2026-05-04",
		"2026-05-05", "2026-06-19", "2026-10-01", "2026-10-02", "2026-10-05",
		"2026-10-06", "2026-10-07",
		// 2027
		"2027-01-01", "2027-02-08", "2027-02-09", "2027-02-10", "2027-02-11",
		"2027-02-12", "2027-04-05", "2027-05-03", "2027-05-04", "2027-05-05",
		"2027-06-14", "2027-10-01", "2027-10-04", "2027-10-05", "2027-10-06",
		"2027-10-07",
	}
	m := make(map[string]bool, len(dates))
	for _, d := range dates {
		m[d] = true
	}
	return m
}()

func isTradingHours(now time.Time) bool {
	local := now.In(ChinaStandardTime)
	weekday := local.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}
	if chinaHolidays[local.Format("2006-01-02")] {
		return false
	}
	minutes := local.Hour()*60 + local.Minute()
	return (minutes >= 9*60+30 && minutes <= 11*60+30) || (minutes >= 13*60 && minutes <= 15*60)
}

func expectedNAVDate(now time.Time) time.Time {
	local := dateOnly(now)
	weekday := local.Weekday()
	if weekday == time.Monday {
		return local.AddDate(0, 0, -3)
	}
	if weekday == time.Sunday {
		return local.AddDate(0, 0, -2)
	}
	return local.AddDate(0, 0, -1)
}

func dateOnly(value time.Time) time.Time {
	local := value.In(ChinaStandardTime)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
}

func sameLocalDate(left, right time.Time) bool {
	return dateOnly(left).Equal(dateOnly(right))
}

func parseSnapshotNAVDate(value string) (time.Time, error) {
	if len(value) >= len("2006-01-02") {
		value = value[:len("2006-01-02")]
	}
	return time.Parse("2006-01-02", value)
}
