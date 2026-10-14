package api

// Round 62 (the round-60 plan's Option B): the orphan-blob sweep admin
// endpoint. POST /api/v1/admin/blob-gc runs one on-demand sweep across
// every blob owner and reports the per-owner outcome. It drives the same
// storage machinery as the round-61 periodic sweep (MessageStore.BlobOwners
// + GCOrphanBlobs), so the identical guarantees apply: the reference
// checker protects blobs referenced by a live Email, and the grace window
// protects fresh blobs. The route is registered with the family's
// withAuth + adminMiddleware double wrap (admin.go).

import (
	"net/http"
)

// blobGCResult is the per-owner outcome of one orphan-blob sweep.
type blobGCResult struct {
	Removed    int   `json:"removed"`
	BytesFreed int64 `json:"bytesFreed"`
}

// handleBlobGC serves POST /api/v1/admin/blob-gc. Without a wired message
// store it reports 503 (the ratelimit nil-manager convention).
func (s *Server) handleBlobGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.sendError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.msgStore == nil {
		s.sendError(w, http.StatusServiceUnavailable, "message store not available")
		return
	}
	owners, err := s.msgStore.BlobOwners()
	if err != nil {
		s.sendError(w, http.StatusInternalServerError, "blob gc sweep failed")
		return
	}
	results := make(map[string]blobGCResult, len(owners))
	total := 0
	for _, owner := range owners {
		before, err := s.msgStore.UserUsage(owner)
		if err != nil {
			s.logger.Warn("admin blob gc: usage failed", "owner", owner, "error", err)
			continue
		}
		n, err := s.msgStore.GCOrphanBlobs(owner)
		if err != nil {
			s.logger.Warn("admin blob gc: owner sweep failed", "owner", owner, "error", err)
			continue
		}
		after, err := s.msgStore.UserUsage(owner)
		if err != nil {
			s.logger.Warn("admin blob gc: usage failed", "owner", owner, "error", err)
			continue
		}
		results[owner] = blobGCResult{Removed: n, BytesFreed: before - after}
		total += n
	}
	s.sendJSON(w, http.StatusOK, map[string]interface{}{
		"totalRemoved": total,
		"results":      results,
	})
}
