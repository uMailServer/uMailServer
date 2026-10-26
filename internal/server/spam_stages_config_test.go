package server

// Regression test: spam.enabled and spam.bayesian.enabled were read by no
// runtime code, so an operator who switched spam filtering off still had mail
// greylisted, scored, rejected and filed into Junk. spamStages now honours
// both switches.

import (
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/umailserver/umailserver/internal/config"
	"github.com/umailserver/umailserver/internal/storage"
)

func spamStageNames(s *Server) []string {
	var names []string
	for _, st := range s.spamStages() {
		names = append(names, st.Name())
	}
	return names
}

func newSpamStagesServer(t *testing.T, spamCfg config.SpamConfig) *Server {
	t.Helper()
	stDB, err := storage.OpenDatabase(filepath.Join(t.TempDir(), "storage.db"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	t.Cleanup(func() { stDB.Close() })
	return &Server{
		config:    &config.Config{Spam: spamCfg},
		storageDB: stDB,
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

func TestSpamStages_HonourConfigSwitches(t *testing.T) {
	base := config.SpamConfig{
		Enabled:         true,
		RejectThreshold: 9,
		JunkThreshold:   3,
		Bayesian:        config.BayesianConfig{Enabled: true},
		Greylisting:     config.GreylistingConfig{Enabled: true},
		RBLServers:      []string{"zen.spamhaus.org"},
	}

	t.Run("all enabled", func(t *testing.T) {
		got := spamStageNames(newSpamStagesServer(t, base))
		if want := []string{"Greylist", "RBL", "Heuristic", "Bayesian", "Score"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("stages = %v, want %v", got, want)
		}
	})

	t.Run("spam disabled", func(t *testing.T) {
		cfg := base
		cfg.Enabled = false
		if got := spamStageNames(newSpamStagesServer(t, cfg)); len(got) != 0 {
			t.Fatalf("spam.enabled=false still built stages %v", got)
		}
	})

	t.Run("bayesian disabled", func(t *testing.T) {
		cfg := base
		cfg.Bayesian.Enabled = false
		cfg.Greylisting.Enabled = false
		cfg.RBLServers = nil
		got := spamStageNames(newSpamStagesServer(t, cfg))
		if want := []string{"Heuristic", "Score"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("stages = %v, want %v", got, want)
		}
	})
}
