package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/db/migrations"
)

func TestDescribeStartupErrorSchemaTooNewF6732(t *testing.T) {
	err := fmt.Errorf("open db: %w", migrations.ErrSchemaTooNew)
	if got := describeStartupError(err); !strings.Contains(got, "upgrade the binary") {
		t.Fatalf("missing upgrade hint: %q", got)
	}
	if got := describeStartupError(errors.New("boom")); got != "boom" {
		t.Fatalf("got %q", got)
	}
}
