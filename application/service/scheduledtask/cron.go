package scheduledtask

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronField struct{ allowed map[int]struct{} }

func validateCron(expr string) error {
	_, err := parseCron(expr)
	return err
}

func cronMatches(expr string, at time.Time) bool {
	fields, err := parseCron(expr)
	if err != nil {
		return false
	}
	values := []int{at.Minute(), at.Hour(), at.Day(), int(at.Month()), int(at.Weekday())}
	for index, value := range values {
		if _, ok := fields[index].allowed[value]; !ok {
			return false
		}
	}
	return true
}

func parseCron(expr string) ([5]cronField, error) {
	var out [5]cronField
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return out, fmt.Errorf("cron must contain 5 fields: minute hour day month weekday")
	}
	ranges := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	for index, part := range parts {
		allowed, err := expandCronField(part, ranges[index][0], ranges[index][1])
		if err != nil {
			return out, fmt.Errorf("invalid cron field %d: %w", index+1, err)
		}
		out[index] = cronField{allowed: allowed}
	}
	return out, nil
}

func expandCronField(raw string, min, max int) (map[int]struct{}, error) {
	out := make(map[int]struct{})
	for _, token := range strings.Split(raw, ",") {
		base, step := token, 1
		if pieces := strings.Split(token, "/"); len(pieces) == 2 {
			base = pieces[0]
			value, err := strconv.Atoi(pieces[1])
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("invalid step")
			}
			step = value
		} else if len(pieces) > 2 {
			return nil, fmt.Errorf("invalid step expression")
		}
		start, end := min, max
		switch {
		case base == "*":
		case strings.Contains(base, "-"):
			bounds := strings.Split(base, "-")
			if len(bounds) != 2 {
				return nil, fmt.Errorf("invalid range")
			}
			var err error
			start, err = strconv.Atoi(bounds[0])
			if err != nil {
				return nil, fmt.Errorf("invalid range start")
			}
			end, err = strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("invalid range end")
			}
		default:
			value, err := strconv.Atoi(base)
			if err != nil {
				return nil, fmt.Errorf("invalid number")
			}
			start, end = value, value
		}
		if start < min || end > max || start > end {
			return nil, fmt.Errorf("value must be between %d and %d", min, max)
		}
		for value := start; value <= end; value += step {
			out[value] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty field")
	}
	return out, nil
}
