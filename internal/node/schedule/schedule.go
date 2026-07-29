package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	StateInWindow    = "in_window"
	StateOutOfWindow = "out_of_window"
)

type ClockTime struct {
	Hour   int
	Minute int
}

type Window struct {
	From  ClockTime
	Until ClockTime
}

func ParseWindow(from, until string) (Window, error) {
	start, err := ParseClockTime(from)
	if err != nil {
		return Window{}, fmt.Errorf("parse from time: %w", err)
	}
	end, err := ParseClockTime(until)
	if err != nil {
		return Window{}, fmt.Errorf("parse until time: %w", err)
	}
	return Window{From: start, Until: end}, nil
}

func ParseClockTime(value string) (ClockTime, error) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return ClockTime{}, fmt.Errorf("expected HH:MM")
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil || hour < 0 || hour > 23 {
		return ClockTime{}, fmt.Errorf("hour must be 00 through 23")
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || minute < 0 || minute > 59 {
		return ClockTime{}, fmt.Errorf("minute must be 00 through 59")
	}
	return ClockTime{Hour: hour, Minute: minute}, nil
}

func (w Window) StateAt(now time.Time, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	current := local.Hour()*60 + local.Minute()
	start := w.From.Hour*60 + w.From.Minute
	end := w.Until.Hour*60 + w.Until.Minute
	if start == end {
		return StateInWindow
	}
	if start < end {
		if current >= start && current < end {
			return StateInWindow
		}
		return StateOutOfWindow
	}
	if current >= start || current < end {
		return StateInWindow
	}
	return StateOutOfWindow
}

func (w Window) String() string {
	return fmt.Sprintf("%02d:%02d-%02d:%02d", w.From.Hour, w.From.Minute, w.Until.Hour, w.Until.Minute)
}
