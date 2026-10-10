package carddav

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// syncTokenPrefix marks sync-tokens issued by this server (RFC 6578 §3.1:
// tokens are URIs); the remainder is the address book's collection tag.
const syncTokenPrefix = "data:,"

type syncQuery struct {
	XMLName   xml.Name `xml:"sync-collection"`
	SyncToken string   `xml:"sync-token"`
	Prop      *Prop    `xml:"prop"`
}

// reportSyncCollection implements DAV:sync-collection (RFC 6578) as in
// CalDAV (F5890). An empty token yields the full membership and a new token.
// No deletion history is kept, so a token equal to the current one yields an
// empty change set and any other is refused with DAV:valid-sync-token
// (§3.2), which makes the client re-sync from scratch.
func (s *Server) reportSyncCollection(w http.ResponseWriter, r *http.Request, username string, body []byte) {
	var q syncQuery
	if err := xml.Unmarshal(body, &q); err != nil {
		s.sendError(w, http.StatusBadRequest, "invalid sync-collection")
		return
	}
	id := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/dav/addressbooks/"), "/", 2)[0]
	ab, err := s.storage.GetAddressbook(username, id)
	if err != nil || ab == nil {
		s.sendError(w, http.StatusForbidden, "address book not found")
		return
	}
	// Taken before listing: a change racing the listing then yields a token
	// older than the data, so the next sync is refused rather than missed.
	token := syncTokenPrefix + fmt.Sprintf("%d", ab.Modified.UnixNano())
	got := strings.TrimSpace(q.SyncToken)
	ms := &Multistatus{SyncToken: token}
	switch got {
	case "":
		contacts, err := s.storage.GetContacts(username, id)
		if err != nil {
			s.sendError(w, http.StatusInternalServerError, "failed to query addressbook contacts")
			return
		}
		for _, c := range contacts {
			if uid := s.extractUIDFromVCard(c); uid != "" {
				ms.Responses = append(ms.Responses, s.buildContactResponse(username, id, uid, c))
			}
		}
		sort.Slice(ms.Responses, func(i, j int) bool { return ms.Responses[i].Href < ms.Responses[j].Href })
	case token:
	default:
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(xml.Header + `<error xmlns="DAV:"><valid-sync-token/></error>`))
		return
	}
	s.writeMultistatus(w, ms, requestFromProp(q.Prop))
}
