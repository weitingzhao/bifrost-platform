package patrol

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type cronField struct {
	all   bool
	vals  map[int]struct{}
	min   int
	max   int
	label string
}

type CronExpr struct {
	raw    string
	minute cronField
	hour   cronField
	dom    cronField
	month  cronField
	dow    cronField
}

// ParseCron parses a 5-field cron expression (min hour dom month dow).
func ParseCron(expr string) (*CronExpr, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("want 5 fields, got %d", len(fields))
	}
	minF, err := parseField(fields[0], 0, 59, "minute")
	if err != nil {
		return nil, err
	}
	hourF, err := parseField(fields[1], 0, 23, "hour")
	if err != nil {
		return nil, err
	}
	domF, err := parseField(fields[2], 1, 31, "dom")
	if err != nil {
		return nil, err
	}
	monF, err := parseField(fields[3], 1, 12, "month")
	if err != nil {
		return nil, err
	}
	dowF, err := parseField(fields[4], 0, 7, "dow")
	if err != nil {
		return nil, err
	}
	// 7 == Sunday in some cron dialects.
	if _, ok := dowF.vals[7]; ok {
		dowF.vals[0] = struct{}{}
		delete(dowF.vals, 7)
	}
	return &CronExpr{raw: expr, minute: minF, hour: hourF, dom: domF, month: monF, dow: dowF}, nil
}

func parseField(raw string, min, max int, label string) (cronField, error) {
	f := cronField{vals: map[int]struct{}{}, min: min, max: max, label: label}
	if raw == "*" {
		f.all = true
		return f, nil
	}
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return f, fmt.Errorf("%s: empty list item", label)
		}
		step := 1
		base := part
		if i := strings.IndexByte(part, '/'); i >= 0 {
			base = part[:i]
			n, err := strconv.Atoi(part[i+1:])
			if err != nil || n <= 0 {
				return f, fmt.Errorf("%s: invalid step %q", label, part)
			}
			step = n
		}
		if base == "*" {
			for v := min; v <= max; v += step {
				f.vals[v] = struct{}{}
			}
			continue
		}
		lo, hi := 0, 0
		if i := strings.IndexByte(base, '-'); i >= 0 {
			a, err1 := strconv.Atoi(base[:i])
			b, err2 := strconv.Atoi(base[i+1:])
			if err1 != nil || err2 != nil {
				return f, fmt.Errorf("%s: invalid range %q", label, base)
			}
			lo, hi = a, b
		} else {
			n, err := strconv.Atoi(base)
			if err != nil {
				return f, fmt.Errorf("%s: invalid value %q", label, base)
			}
			lo, hi = n, n
		}
		if lo < min || hi > max || lo > hi {
			return f, fmt.Errorf("%s: %d-%d out of range %d-%d", label, lo, hi, min, max)
		}
		for v := lo; v <= hi; v += step {
			f.vals[v] = struct{}{}
		}
	}
	if len(f.vals) == 0 {
		return f, fmt.Errorf("%s: no values", label)
	}
	return f, nil
}

func (f cronField) match(v int) bool {
	if f.all {
		return true
	}
	_, ok := f.vals[v]
	return ok
}

func (c *CronExpr) matches(t time.Time) bool {
	t = t.UTC()
	if !c.minute.match(t.Minute()) || !c.hour.match(t.Hour()) || !c.month.match(int(t.Month())) {
		return false
	}
	domStar := c.dom.all
	dowStar := c.dow.all
	domOK := c.dom.match(t.Day())
	dowOK := c.dow.match(int(t.Weekday()))
	switch {
	case domStar && dowStar:
		return true
	case !domStar && dowStar:
		return domOK
	case domStar && !dowStar:
		return dowOK
	default:
		return domOK || dowOK
	}
}

// NextAfter returns the next UTC minute strictly after from that matches expr.
func NextAfter(expr string, from time.Time) (time.Time, error) {
	c, err := ParseCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	return c.NextAfter(from)
}

func (c *CronExpr) NextAfter(from time.Time) (time.Time, error) {
	t := from.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.Add(400 * 24 * time.Hour)
	for !t.After(limit) {
		if c.matches(t) {
			return t, nil
		}
		t = t.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("no cron match within 400d for %q", c.raw)
}
