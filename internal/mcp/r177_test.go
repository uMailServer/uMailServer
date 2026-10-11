package mcp

import "testing"

func TestR177TokenEqual(t *testing.T) {
	if !tokenEqual("abc", "abc") || tokenEqual("abd", "abc") || tokenEqual("", "") || tokenEqual("x", "") || tokenEqual("abcd", "abc") {
		t.Fatal("tokenEqual wrong")
	}
}
