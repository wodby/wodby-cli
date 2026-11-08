package wodby1

import (
	"strconv"
	"strings"
)

// minimumCronIntervalMinutes is the shortest time Wodby 2 allows between two
// runs of a new or changed cron schedule. Wodby 1 allowed schedules down to
// every minute.
const minimumCronIntervalMinutes = 5

// minimumIntervalMinuteField replaces the minute field of a schedule that runs
// too often.
const minimumIntervalMinuteField = "*/5"

// targetCrontab returns the Wodby 2 schedule for a Wodby 1 cron: the schedule
// from migratedCrontab, with the minute field replaced by "*/5" when two runs
// could start less than 5 minutes apart, which Wodby 2 refuses. The other
// fields stay, so "*/2 4-20 * * *" becomes "*/5 4-20 * * *". adjusted reports
// the replacement.
//
// Names of migrated schedules are derived from migratedCrontab, not from this
// value, so schedules created before the adjustment keep their names and a
// resumed migration updates them in place.
func targetCrontab(value string) (crontab string, adjusted bool) {
	crontab = migratedCrontab(value)
	fields := strings.Fields(crontab)
	if len(fields) != 5 {
		return crontab, false
	}
	minutes, ok := cronFieldValues(fields[0], 0, 59)
	if !ok {
		return crontab, false
	}
	hours, ok := cronFieldValues(fields[1], 0, 23)
	if !ok {
		return crontab, false
	}
	if shortestCronIntervalMinutes(minutes, hours) >= minimumCronIntervalMinutes {
		return crontab, false
	}
	fields[0] = minimumIntervalMinuteField
	return strings.Join(fields, " "), true
}

// shortestCronIntervalMinutes returns the shortest time in minutes between two
// runs, capped at 60: two runs in the same hour, or the last run of an hour
// and the first of the next one when both hours match. Hour 23 and hour 0
// count as neighbors. It mirrors the check Wodby 2 applies.
func shortestCronIntervalMinutes(minutes, hours []bool) int {
	shortest := 60
	first, last, previous := -1, -1, -1
	for minute, set := range minutes {
		if !set {
			continue
		}
		if first < 0 {
			first = minute
		}
		if previous >= 0 && minute-previous < shortest {
			shortest = minute - previous
		}
		previous, last = minute, minute
	}
	if first < 0 {
		return shortest
	}
	for hour := range hours {
		if hours[hour] && hours[(hour+1)%len(hours)] {
			if wrap := 60 - last + first; wrap < shortest {
				shortest = wrap
			}
			break
		}
	}
	return shortest
}

// cronFieldValues returns which values from minValue to maxValue a numeric
// crontab field matches. It accepts lists of "*", "N", "N-M", each optionally
// with "/step". Anything else, such as names or "?", is reported as not ok and
// the schedule is left to Wodby 2 to validate.
func cronFieldValues(field string, minValue, maxValue int) ([]bool, bool) {
	values := make([]bool, maxValue+1)
	for _, item := range strings.Split(field, ",") {
		rangePart, step := item, 1
		if index := strings.Index(item, "/"); index >= 0 {
			parsed, err := strconv.Atoi(item[index+1:])
			if err != nil || parsed < 1 {
				return nil, false
			}
			rangePart, step = item[:index], parsed
		}
		start, end := minValue, maxValue
		switch {
		case rangePart == "*":
		case strings.Contains(rangePart, "-"):
			bounds := strings.SplitN(rangePart, "-", 2)
			from, errFrom := strconv.Atoi(bounds[0])
			to, errTo := strconv.Atoi(bounds[1])
			if errFrom != nil || errTo != nil {
				return nil, false
			}
			start, end = from, to
		default:
			value, err := strconv.Atoi(rangePart)
			if err != nil {
				return nil, false
			}
			start, end = value, value
			if step > 1 {
				end = maxValue
			}
		}
		if start < minValue || end > maxValue || start > end {
			return nil, false
		}
		for value := start; value <= end; value += step {
			values[value] = true
		}
	}
	return values, true
}
