package cli

import (
	"testing"
)

// --- getString and getInt64 direct tests ---

func TestGetString_Found(t *testing.T) {
	m := map[string]interface{}{
		"key": "value",
	}
	result := getString(m, "key")
	if result != "value" {
		t.Errorf("Expected 'value', got %q", result)
	}
}

func TestGetString_NotFound(t *testing.T) {
	m := map[string]interface{}{
		"other": "value",
	}
	result := getString(m, "key")
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestGetString_WrongType(t *testing.T) {
	m := map[string]interface{}{
		"key": 123, // not a string
	}
	result := getString(m, "key")
	if result != "" {
		t.Errorf("Expected empty string for wrong type, got %q", result)
	}
}

func TestGetInt64_Int64(t *testing.T) {
	m := map[string]interface{}{
		"key": int64(42),
	}
	result := getInt64(m, "key")
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestGetInt64_Float64(t *testing.T) {
	m := map[string]interface{}{
		"key": float64(42.5),
	}
	result := getInt64(m, "key")
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestGetInt64_Int(t *testing.T) {
	m := map[string]interface{}{
		"key": int(42),
	}
	result := getInt64(m, "key")
	if result != 42 {
		t.Errorf("Expected 42, got %d", result)
	}
}

func TestGetInt64_NotFound(t *testing.T) {
	m := map[string]interface{}{
		"other": 42,
	}
	result := getInt64(m, "key")
	if result != 0 {
		t.Errorf("Expected 0, got %d", result)
	}
}

func TestGetInt64_WrongType(t *testing.T) {
	m := map[string]interface{}{
		"key": "string",
	}
	result := getInt64(m, "key")
	if result != 0 {
		t.Errorf("Expected 0 for wrong type, got %d", result)
	}
}
