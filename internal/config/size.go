package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Size represents a byte size that can be parsed from strings like "5GB", "50MB"
type Size int64

const (
	KB Size = 1024
	MB      = 1024 * KB
	GB      = 1024 * MB
	TB      = 1024 * GB
)

// UnmarshalYAML implements yaml.Unmarshaler
func (s *Size) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		// Try unmarshaling as bytes directly
		var bytes int64
		if err := unmarshal(&bytes); err != nil {
			return err
		}
		*s = Size(bytes)
		return nil
	}

	size, err := ParseSize(raw)
	if err != nil {
		return err
	}
	*s = size
	return nil
}

// MarshalYAML implements yaml.Marshaler
func (s Size) MarshalYAML() (interface{}, error) {
	return s.String(), nil
}

// String returns human-readable size
func (s Size) String() string {
	switch {
	case s >= TB && s%TB == 0:
		return fmt.Sprintf("%dTB", s/TB)
	case s >= GB && s%GB == 0:
		return fmt.Sprintf("%dGB", s/GB)
	case s >= MB && s%MB == 0:
		return fmt.Sprintf("%dMB", s/MB)
	case s >= KB && s%KB == 0:
		return fmt.Sprintf("%dKB", s/KB)
	default:
		return fmt.Sprintf("%d", s)
	}
}

// ParseSize parses a size string like "5GB", "50MB", "1024"
func ParseSize(s string) (Size, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}

	// Regex to match number + optional unit
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([KMGT]B?|B?)$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		// Try parsing as plain number
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid size format: %s", s)
		}
		return Size(n), nil
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size value: %s", matches[1])
	}

	unit := matches[2]
	if unit == "" || unit == "B" {
		return Size(value), nil
	}

	switch unit {
	case "K", "KB":
		return Size(value * float64(KB)), nil
	case "M", "MB":
		return Size(value * float64(MB)), nil
	case "G", "GB":
		return Size(value * float64(GB)), nil
	case "T", "TB":
		return Size(value * float64(TB)), nil
	default:
		return 0, fmt.Errorf("unknown size unit: %s", unit)
	}
}

// Duration is a wrapper around time.Duration that can be parsed from strings
type Duration time.Duration

// UnmarshalYAML implements yaml.Unmarshaler
func (d *Duration) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var raw string
	if err := unmarshal(&raw); err != nil {
		// Try unmarshaling as nanoseconds directly
		var ns int64
		if err := unmarshal(&ns); err != nil {
			return err
		}
		*d = Duration(ns)
		return nil
	}

	dur, err := time.ParseDuration(raw)
	if err != nil {
		return fmt.Errorf("invalid duration: %s", raw)
	}
	*d = Duration(dur)
	return nil
}

// MarshalYAML implements yaml.Marshaler
func (d Duration) MarshalYAML() (interface{}, error) {
	return time.Duration(d).String(), nil
}

// String returns the duration as a string
func (d Duration) String() string {
	return time.Duration(d).String()
}

// ToDuration converts to time.Duration
func (d Duration) ToDuration() time.Duration {
	return time.Duration(d)
}
