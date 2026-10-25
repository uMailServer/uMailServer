package jmap

// Round 61: background orphan-blob sweep wiring (the round-60 plan,
// SAGE 01M4TGHTKR4EVMJN9BCPEZD10F). GCOrphanBlobs (internal/storage/
// messagestore.go:438) existed with zero production callers; this file
// gives it one, mirroring StartUploadGC's ticker/stop pattern. The sweep
// is safe by construction: GCOrphanBlobs refuses to run without the
// reference checker (installed at wiring time, server.go:164), keeps
// blobs younger than orphanGCGrace, and removes only blobs the checker
// reports as unreferenced — identical-content blobs shared with a live
// Email survive (round 57's release mechanism uses the same checker).

import (
	"time"
)

// defaultBlobGCEvery is the orphan-blob sweep cadence when
// Config.BlobGCInterval is zero (NewServer applies the same default).
const defaultBlobGCEvery = time.Hour

// StartBlobGC starts the background orphan-blob sweep (round 61). It is
// idempotent and shares the upload GC lifecycle: Stop ends both, and after
// Stop neither restarts.
func (s *Server) StartBlobGC() {
	g := &s.blobGC
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started || g.stopped {
		return
	}
	g.started = true
	g.stop = make(chan struct{})
	g.done = make(chan struct{})
	every := s.config.BlobGCInterval
	if every <= 0 {
		every = defaultBlobGCEvery
	}
	go func(stop <-chan struct{}, done chan<- struct{}) {
		defer close(done)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				_, _ = s.sweepBlobGC()
			}
		}
	}(g.stop, g.done)
}

// sweepBlobGC runs one orphan-blob sweep across every blob owner in the
// message store and returns the total number of blobs removed. Owners are
// the store root's directory entries (MessageStore.BlobOwners). A failing
// owner is logged and skipped so one bad user cannot block the sweep.
func (s *Server) sweepBlobGC() (int, error) {
	if s.msgStore == nil {
		return 0, nil
	}
	owners, err := s.msgStore.BlobOwners()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, owner := range owners {
		n, err := s.msgStore.GCOrphanBlobs(owner)
		if err != nil {
			s.logger.Warn("blob gc: owner sweep failed", "owner", owner, "error", err)
			continue
		}
		total += n
	}
	// PruneChanges enforces the 30-day retention and the per-user entry cap
	// on the change journals (F7390's in-transaction live count). Before
	// this wiring it had no production caller, so journals grew unbounded
	// despite the implemented policy; the sweep iterates the journal owners
	// directly (ChangesUsers), not the blob owners, so journal-only users
	// are covered too.
	changesUsers, err := s.db.ChangesUsers()
	if err != nil {
		s.logger.Warn("changes gc: user enumeration failed", "error", err)
		return total, err
	}
	for _, owner := range changesUsers {
		if err := s.db.PruneChanges(owner); err != nil {
			s.logger.Warn("changes gc: journal prune failed", "owner", owner, "error", err)
		}
	}
	if total > 0 {
		s.logger.Info("blob gc sweep complete", "owners", len(owners), "removed", total)
	}
	return total, nil
}
