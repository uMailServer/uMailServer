package server

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/config"
)

// F6890: server.graceful_timeout controls the shutdown background wait.
func TestR212GracefulTimeout(t *testing.T) {
	s := &Server{config: &config.Config{}}
	if got := s.gracefulTimeout(); got != 10*time.Second {
		t.Fatalf("default = %v", got)
	}
	s.config.Server.GracefulTimeout = 45
	if got := s.gracefulTimeout(); got != 45*time.Second {
		t.Fatalf("configured = %v", got)
	}
}
