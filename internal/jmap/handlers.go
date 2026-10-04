package jmap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// validateAccountId checks if the provided accountId matches the authenticated user.
// If accountId is provided and doesn't match, returns an error Response.
func validateAccountId(accountId, user, methodName, callID string) (bool, Response) {
	if accountId != "" && accountId != user {
		return false, Response{
			Name: methodName,
			Args: map[string]interface{}{
				"type":        "accountNotFound",
				"description": "Account not found or access denied",
			},
			ID: callID,
		}
	}
	return true, Response{}
}

// handleMailboxGet handles Mailbox/get method
func (s *Server) handleMailboxGet(user string, call MethodCall) Response {
	args := call.Args

	accountID, _ := args["accountId"].(string)
	ids, _ := args["ids"].([]interface{})

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Mailbox/get", call.ID); !valid {
		return resp
	}

	// Get mailboxes from storage
	mailboxNames, err := s.db.ListMailboxes(user)
	if err != nil {
		return Response{
			Name: "Mailbox/get",
			Args: map[string]interface{}{
				"accountId": accountID,
				"state":     s.stateToken(user),
				"list":      []Mailbox{},
				"notFound":  []string{},
			},
			ID: call.ID,
		}
	}

	var mailboxes []Mailbox
	for _, name := range mailboxNames {
		mboxID := getMailboxIDFromName(name)

		// Get message counts
		total, _, unseen, err := s.db.GetMailboxCounts(user, name)
		if err != nil {
			total, unseen = 0, 0
		}

		// Get thread counts (use total as approximation)
		threads := total

		// Determine role and rights based on mailbox name
		role := ""
		mayDelete := true
		mayRename := true

		switch name {
		case "INBOX":
			role = "inbox"
			mayDelete = false
			mayRename = false
		case "Sent":
			role = "sent"
		case "Drafts":
			role = "drafts"
		case "Trash":
			role = "trash"
		case "Junk":
			role = "junk"
		case "Archive":
			role = "archive"
		}

		mailbox := Mailbox{
			ID:            mboxID,
			Name:          name,
			Role:          role,
			SortOrder:     0,
			TotalEmails:   total,
			UnreadEmails:  unseen,
			TotalThreads:  threads,
			UnreadThreads: unseen, // Approximation
			MyRights: MailboxRights{
				MayReadItems:   true,
				MayAddItems:    true,
				MayRemoveItems: true,
				MaySetSeen:     true,
				MaySetKeywords: true,
				MayCreateChild: true,
				MayRename:      mayRename,
				MayDelete:      mayDelete,
				MaySubmit:      true,
			},
			IsSubscribed: true,
		}
		mailboxes = append(mailboxes, mailbox)
	}

	// Filter by IDs if specified
	var result []Mailbox
	var notFound []string
	if len(ids) > 0 {
		idSet := make(map[string]bool)
		for _, id := range ids {
			if str, ok := id.(string); ok {
				idSet[str] = true
			}
		}
		foundIDs := make(map[string]bool)
		for _, mbox := range mailboxes {
			if idSet[mbox.ID] {
				result = append(result, mbox)
				foundIDs[mbox.ID] = true
			}
		}
		// RFC 8620 §4.2: requested ids with no matching record are reported
		// in notFound rather than silently dropped.
		for id := range idSet {
			if !foundIDs[id] {
				notFound = append(notFound, id)
			}
		}
	} else {
		result = mailboxes
	}

	return Response{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"state":     s.stateToken(user),
			"list":      result,
			"notFound":  notFound,
		},
		ID: call.ID,
	}
}

// handleMailboxQuery handles Mailbox/query method
func (s *Server) handleMailboxQuery(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Mailbox/query", call.ID); !valid {
		return resp
	}

	fullIDs := s.runMailboxQuery(user)

	// Snapshot the ordered result for Mailbox/queryChanges deltas
	// (RFC 8620 §5.6), stamped with the current state sequence.
	queryState := s.stateToken(user)
	_ = s.db.SaveQuerySnapshot(user, &storage.QuerySnapshot{
		QueryHash: querySnapshotHash("Mailbox/query", nil, nil),
		Seq:       storage.ParseChangeState(queryState),
		IDs:       fullIDs,
	})

	return Response{
		Name: "Mailbox/query",
		Args: map[string]interface{}{
			"accountId":           accountID,
			"queryState":          queryState,
			"canCalculateChanges": false,
			"position":            0,
			"total":               len(fullIDs),
			"ids":                 fullIDs,
		},
		ID: call.ID,
	}
}

// runMailboxQuery computes the full ordered result of a Mailbox/query — the
// canonical mailbox ids — shared by Mailbox/query and the Mailbox/queryChanges
// delta computation. Storage errors yield an empty result, matching the
// handler's existing error-path behavior.
func (s *Server) runMailboxQuery(user string) []string {
	mailboxNames, err := s.db.ListMailboxes(user)
	if err != nil {
		return []string{}
	}
	ids := make([]string, 0, len(mailboxNames))
	for _, name := range mailboxNames {
		ids = append(ids, getMailboxIDFromName(name))
	}
	return ids
}

// handleMailboxSet handles Mailbox/set method
func (s *Server) handleMailboxSet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Mailbox/set", call.ID); !valid {
		return resp
	}

	// Parse create, update, destroy
	create, _ := args["create"].(map[string]interface{})
	update, _ := args["update"].(map[string]interface{})
	destroy, _ := args["destroy"].([]interface{})

	created := make(map[string]Mailbox)
	notCreated := make(map[string]interface{})
	updated := make(map[string]interface{})
	notUpdated := make(map[string]interface{})
	var destroyed []string
	notDestroyed := make(map[string]interface{})

	// Handle create
	for key, val := range create {
		createData, ok := val.(map[string]interface{})
		if !ok {
			notCreated[key] = map[string]interface{}{
				"type": "invalidArguments",
			}
			continue
		}

		name, _ := createData["name"].(string)
		if name == "" {
			notCreated[key] = map[string]interface{}{
				"type":        "invalidArguments",
				"description": "Mailbox name is required",
			}
			continue
		}

		if err := s.db.CreateMailbox(user, name); err != nil {
			notCreated[key] = map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("CreateMailbox", err),
			}
			continue
		}

		mboxID := getMailboxIDFromName(name)
		created[key] = Mailbox{
			ID:   mboxID,
			Name: name,
		}
	}

	// Handle update
	for key, val := range update {
		updateData, ok := val.(map[string]interface{})
		if !ok {
			notUpdated[key] = map[string]interface{}{
				"type": "invalidArguments",
			}
			continue
		}

		// Get old name from ID
		oldName := getMailboxNameFromID(key)

		// Check for rename
		if newName, ok := updateData["name"].(string); ok && newName != "" && newName != oldName {
			if err := s.db.RenameMailbox(user, oldName, newName); err != nil {
				notUpdated[key] = map[string]interface{}{
					"type":        "serverFail",
					"description": s.safeError("RenameMailbox", err),
				}
				continue
			}
		}

		updated[key] = map[string]interface{}{}
	}

	// Handle destroy
	for _, id := range destroy {
		if idStr, ok := id.(string); ok {
			name := getMailboxNameFromID(idStr)
			if err := s.db.DeleteMailbox(user, name); err != nil {
				notDestroyed[idStr] = map[string]interface{}{
					"type":        "serverFail",
					"description": s.safeError("DeleteMailbox", err),
				}
			} else {
				destroyed = append(destroyed, idStr)
			}
		}
	}

	return Response{
		Name: "Mailbox/set",
		Args: map[string]interface{}{
			"accountId":    accountID,
			"oldState":     nil,
			"newState":     s.stateToken(user),
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		},
		ID: call.ID,
	}
}

// handleEmailGet handles Email/get method
func (s *Server) handleEmailGet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	ids, _ := args["ids"].([]interface{})

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Email/get", call.ID); !valid {
		return resp
	}

	var emails []Email
	var notFound []string

	// Get list of mailboxes for this user
	mailboxes, _ := s.db.ListMailboxes(user)

	for _, id := range ids {
		if idStr, ok := id.(string); ok {
			// Try to find the message in any mailbox
			var found bool
			for _, mbox := range mailboxes {
				uids, _ := s.db.GetMessageUIDs(user, mbox)
				for _, uid := range uids {
					meta, err := s.db.GetMessageMetadata(user, mbox, uid)
					if err != nil || meta == nil {
						continue
					}
					if meta.MessageID == idStr {
						email := storageToJMAPEmail(meta, nil, mbox)
						emails = append(emails, email)
						found = true
						break
					}
				}
				if found {
					break
				}
			}
			if !found {
				notFound = append(notFound, idStr)
			}
		}
	}

	return Response{
		Name: "Email/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"state":     s.stateToken(user),
			"list":      emails,
			"notFound":  notFound,
		},
		ID: call.ID,
	}
}

// handleEmailQuery handles Email/query method
func (s *Server) handleEmailQuery(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Email/query", call.ID); !valid {
		return resp
	}

	filter := args["filter"]
	sort, _ := args["sort"].([]interface{})
	position, _ := args["position"].(float64)
	limit, _ := args["limit"].(float64)

	// Default limit
	if limit == 0 || limit > 100 {
		limit = 30
	}

	fullIDs := s.runEmailQuery(user, filter, sort)

	// Snapshot the full ordered result for Email/queryChanges deltas
	// (RFC 8620 §5.6), keyed by the filter/sort pair and stamped with the
	// current state sequence.
	queryState := s.stateToken(user)
	_ = s.db.SaveQuerySnapshot(user, &storage.QuerySnapshot{
		QueryHash: querySnapshotHash("Email/query", filter, sort),
		Seq:       storage.ParseChangeState(queryState),
		IDs:       fullIDs,
	})

	// Apply position and limit. position is a client-supplied offset and may
	// arrive negative. Clamping only the upper bound would leave start
	// negative and make the ids loop a negative-index panic, so floor it at
	// 0 first.
	total := len(fullIDs)
	start := int(position)
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + int(limit)
	if end > total {
		end = total
	}

	var ids []string
	for i := start; i < end; i++ {
		ids = append(ids, fullIDs[i])
	}

	return Response{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId":           accountID,
			"queryState":          queryState,
			"canCalculateChanges": false,
			"position":            int(position),
			"total":               total,
			"ids":                 ids,
		},
		ID: call.ID,
	}
}

// runEmailQuery computes the full ordered result of an Email/query for a
// filter/sort pair, independent of paging — shared by Email/query and the
// Email/queryChanges delta computation.
func (s *Server) runEmailQuery(user string, filter interface{}, sort interface{}) []string {
	// Parse filter
	filterCondition := parseFilter(filter)
	sortList, _ := sort.([]interface{})

	// Get all messages from mailboxes
	var allMessages []struct {
		id      string
		mailbox string
		uid     uint32
		meta    *storage.MessageMetadata
	}

	mailboxes, _ := s.db.ListMailboxes(user)
	targetMbox := ""
	seenIDs := make(map[string]bool)

	// If filter specifies a mailbox, only query that one
	if filterCondition != nil && filterCondition.InMailbox != "" {
		targetMbox = getMailboxNameFromID(filterCondition.InMailbox)
	}

	for _, mbox := range mailboxes {
		// Skip if filtering to a specific mailbox and this isn't it
		if targetMbox != "" && mbox != targetMbox {
			continue
		}

		uids, _ := s.db.GetMessageUIDs(user, mbox)
		for _, uid := range uids {
			meta, err := s.db.GetMessageMetadata(user, mbox, uid)
			if err != nil || meta == nil {
				continue
			}

			// Apply filters
			if !matchesFilter(meta, filterCondition) {
				continue
			}
			if seenIDs[meta.MessageID] {
				continue
			}
			seenIDs[meta.MessageID] = true

			allMessages = append(allMessages, struct {
				id      string
				mailbox string
				uid     uint32
				meta    *storage.MessageMetadata
			}{
				id:      meta.MessageID,
				mailbox: mbox,
				uid:     uid,
				meta:    meta,
			})
		}
	}

	// Apply sorting
	if len(sortList) > 0 {
		// Parse first sort comparator
		data, _ := json.Marshal(sortList[0])
		var comp Comparator
		_ = json.Unmarshal(data, &comp)

		// Sort messages
		sortMessages(allMessages, comp)
	}

	fullIDs := make([]string, len(allMessages))
	for i := range allMessages {
		fullIDs[i] = allMessages[i].id
	}
	return fullIDs
}

// querySnapshotHash derives the snapshot storage key for a query method and
// its filter/sort pair. The method is part of the key so different query
// types never collide on the same snapshot.
func querySnapshotHash(method string, filter interface{}, sort interface{}) string {
	data, _ := json.Marshal(map[string]interface{}{"method": method, "filter": filter, "sort": sort})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// matchesFilter checks if a message matches the filter condition
func matchesFilter(meta *storage.MessageMetadata, filter *FilterCondition) bool {
	if filter == nil {
		return true
	}

	// Filter by unread
	if filter.NotKeyword == "$seen" {
		if storage.HasFlag(meta.Flags, "\\Seen") {
			return false
		}
	}

	// Filter by text in subject/from/to
	if filter.Text != "" {
		text := strings.ToLower(filter.Text)
		if !strings.Contains(strings.ToLower(meta.Subject), text) &&
			!strings.Contains(strings.ToLower(meta.From), text) &&
			!strings.Contains(strings.ToLower(meta.To), text) {
			return false
		}
	}

	// Filter by subject
	if filter.Subject != "" {
		if !strings.Contains(strings.ToLower(meta.Subject), strings.ToLower(filter.Subject)) {
			return false
		}
	}

	// Filter by from
	if filter.From != "" {
		if !strings.Contains(strings.ToLower(meta.From), strings.ToLower(filter.From)) {
			return false
		}
	}

	// Filter by to
	if filter.To != "" {
		if !strings.Contains(strings.ToLower(meta.To), strings.ToLower(filter.To)) {
			return false
		}
	}

	// Filter by minSize
	if filter.MinSize > 0 && meta.Size < filter.MinSize {
		return false
	}

	// Filter by maxSize
	if filter.MaxSize > 0 && meta.Size > filter.MaxSize {
		return false
	}

	// Filter by after date
	if filter.After != "" {
		if after, err := time.Parse(time.RFC3339, filter.After); err == nil {
			if meta.InternalDate.Before(after) {
				return false
			}
		}
	}

	// Filter by before date
	if filter.Before != "" {
		if before, err := time.Parse(time.RFC3339, filter.Before); err == nil {
			if meta.InternalDate.After(before) {
				return false
			}
		}
	}

	return true
}

// sortMessages sorts messages based on comparator
func sortMessages(messages []struct {
	id      string
	mailbox string
	uid     uint32
	meta    *storage.MessageMetadata
}, comp Comparator) {
	if comp.Property == "" {
		comp.Property = "receivedAt"
	}

	// Honor the comparator's isAscending exactly as the client requested.
	// The Go zero value (false) is already descending, so a date sort with an
	// omitted isAscending naturally defaults to descending without inverting an
	// explicit ascending request. RFC 8620 §2.7.1 requires the sort order in
	// the request to be honored.
	ascending := comp.IsAscending

	sort.Slice(messages, func(i, j int) bool {
		a, b := messages[i].meta, messages[j].meta
		var less bool

		switch comp.Property {
		case "receivedAt":
			less = a.InternalDate.Before(b.InternalDate)
		case "sentAt":
			less = a.Date < b.Date
		case "from":
			less = strings.ToLower(a.From) < strings.ToLower(b.From)
		case "to":
			less = strings.ToLower(a.To) < strings.ToLower(b.To)
		case "subject":
			less = strings.ToLower(a.Subject) < strings.ToLower(b.Subject)
		case "size":
			less = a.Size < b.Size
		default:
			less = a.InternalDate.Before(b.InternalDate)
		}

		if ascending {
			return less
		}
		return !less
	})
}

// jmapKeywordToIMAPFlag maps a JMAP keyword to its IMAP system flag.
// Keywords without a system-flag equivalent (and keywords this server does
// not model) are reported as unmapped.
func jmapKeywordToIMAPFlag(keyword string) (string, bool) {
	switch keyword {
	case "$seen":
		return "\\Seen", true
	case "$answered":
		return "\\Answered", true
	case "$flagged":
		return "\\Flagged", true
	case "$draft":
		return "\\Draft", true
	}
	return "", false
}

// hasIMAPFlag reports whether flags already contains want.
func hasIMAPFlag(flags []string, want string) bool {
	for _, f := range flags {
		if f == want {
			return true
		}
	}
	return false
}

// removeIMAPFlag returns flags without want, preserving order.
func removeIMAPFlag(flags []string, want string) []string {
	out := flags[:0]
	for _, f := range flags {
		if f != want {
			out = append(out, f)
		}
	}
	return out
}

// handleEmailSet handles Email/set method
func (s *Server) handleEmailSet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Email/set", call.ID); !valid {
		return resp
	}

	// The account's state token is the change-journal sequence (RFC 8620
	// §2): Email/changes accepts and returns it. RFC 8620 §4.3 requires the
	// /set response to echo the state the request was applied to (oldState)
	// and the state after it (newState) in that same format.
	oldState, stateErr := s.db.CurrentChangeState(user)
	if stateErr != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("CurrentChangeState", stateErr),
			},
			ID: call.ID,
		}
	}

	// RFC 8620 §4.3 (/set): if ifInState is supplied and does not match the
	// current state, the request MUST be rejected with a stateMismatch error
	// (§3.6.1 method-level error) and no create/update/destroy processed.
	if ifInState, _ := args["ifInState"].(string); ifInState != "" && ifInState != oldState {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}

	// Parse create, update, destroy
	create, _ := args["create"].(map[string]interface{})
	update, _ := args["update"].(map[string]interface{})
	destroy, _ := args["destroy"].([]interface{})

	created := make(map[string]Email)
	notCreated := make(map[string]interface{})
	updated := make(map[string]interface{})
	notUpdated := make(map[string]interface{})
	var destroyed []string
	notDestroyed := make(map[string]interface{})

	// Handle create (emails are typically imported, not created directly)
	for key := range create {
		notCreated[key] = map[string]interface{}{
			"type":        "notSupported",
			"description": "Use Email/import to create emails",
		}
	}

	// Handle update - update keywords (flags) and mailboxIds
	for emailID, val := range update {
		updateData, ok := val.(map[string]interface{})
		if !ok {
			notUpdated[emailID] = map[string]interface{}{
				"type": "invalidArguments",
			}
			continue
		}

		// Find the message in user's mailboxes
		mailboxes, _ := s.db.ListMailboxes(user)
		var found bool
		var targetMbox string
		var targetUID uint32
		var meta *storage.MessageMetadata

		for _, mbox := range mailboxes {
			uids, _ := s.db.GetMessageUIDs(user, mbox)
			for _, uid := range uids {
				m, err := s.db.GetMessageMetadata(user, mbox, uid)
				if err != nil || m == nil {
					continue
				}
				if m.MessageID == emailID {
					found = true
					targetMbox = mbox
					targetUID = uid
					meta = m
					break
				}
			}
			if found {
				break
			}
		}

		if !found || meta == nil {
			notUpdated[emailID] = map[string]interface{}{
				"type": "notFound",
			}
			continue
		}

		// Update keywords (flags). RFC 8620 §4.3 patches the Email with a
		// PatchObject: the "keywords" property form is a full property set
		// (replace), while the "keywords/$name" path form adds or removes a
		// single keyword and preserves the rest. Both forms are honoured; the
		// path form alone used to be silently ignored.
		if keywords, ok := updateData["keywords"].(map[string]interface{}); ok {
			// Convert JMAP keywords to IMAP flags
			newFlags := []string{}
			for kw, val := range keywords {
				if b, ok := val.(bool); ok && b {
					if flag, ok := jmapKeywordToIMAPFlag(kw); ok {
						newFlags = append(newFlags, flag)
					}
				}
			}
			meta.Flags = newFlags
		}
		for key, val := range updateData {
			const keywordPathPrefix = "keywords/"
			if !strings.HasPrefix(key, keywordPathPrefix) {
				continue
			}
			flag, ok := jmapKeywordToIMAPFlag(strings.TrimPrefix(key, keywordPathPrefix))
			if !ok {
				continue
			}
			// null (JSON null) or false removes the keyword; true adds it.
			// Anything else is an invalid patch value — RFC 8620 §4.3 requires
			// such an update to be rejected with invalidPatch instead of being
			// silently treated as a removal.
			if val == nil {
				meta.Flags = removeIMAPFlag(meta.Flags, flag)
				continue
			}
			if set, isBool := val.(bool); isBool {
				if set {
					if !hasIMAPFlag(meta.Flags, flag) {
						meta.Flags = append(meta.Flags, flag)
					}
				} else {
					meta.Flags = removeIMAPFlag(meta.Flags, flag)
				}
				continue
			}
			notUpdated[emailID] = map[string]interface{}{
				"type": "invalidPatch",
			}
			continue
		}

		// RFC 8620 §4.3: an invalid patch rejects the whole update for this
		// object. Do not apply any further part of the patch (mailboxIds
		// move, metadata persistence) and do not report it as updated —
		// otherwise the response lists the id in both updated and notUpdated
		// while storage changes on a failed update.
		if _, failed := notUpdated[emailID]; failed {
			continue
		}

		// Update mailboxIds (move message)
		if mailboxIDs, ok := updateData["mailboxIds"].(map[string]interface{}); ok {
			// Determine target mailbox
			var newMbox string
			for id, val := range mailboxIDs {
				if b, ok := val.(bool); ok && b {
					newMbox = getMailboxNameFromID(id)
					break
				}
			}

			if newMbox != "" && newMbox != targetMbox {
				// RFC 8620 §2.3: the destination mailbox must exist; RFC 8621
				// §4.4 requires such moves to fail with "mailboxNotFound".
				// getMailboxNameFromID passes unknown ids through verbatim,
				// so without this check the storage layer would silently
				// materialize a phantom mailbox (CreateBucketIfNotExists).
				if !s.mailboxExists(user, newMbox) {
					notUpdated[emailID] = map[string]interface{}{
						"type": "mailboxNotFound",
					}
					continue
				}

				// Move message to new mailbox
				// Store in new mailbox
				newUID, _ := s.db.GetNextUID(user, newMbox)
				if err := s.db.StoreMessageMetadata(user, newMbox, newUID, meta); err != nil {
					notUpdated[emailID] = map[string]interface{}{
						"type":        "serverFail",
						"description": s.safeError("StoreMessageMetadata", err),
					}
					continue
				}

				// Delete from old mailbox
				_ = s.db.DeleteMessage(user, targetMbox, targetUID)
				targetMbox = newMbox
				// The message now lives in the new mailbox under newUID. Point
				// targetUID (and the stored UID) at newUID so the metadata save
				// below updates the moved message instead of writing to the
				// stale UID, which would clobber an unrelated message that
				// already occupies that UID in the destination mailbox.
				targetUID = newUID
				meta.UID = newUID
			}
		}

		// Save updated metadata
		if err := s.db.UpdateMessageMetadata(user, targetMbox, targetUID, meta); err != nil {
			notUpdated[emailID] = map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("UpdateMessageMetadata", err),
			}
			continue
		}

		updated[emailID] = map[string]interface{}{}
	}

	// Handle destroy
	for _, id := range destroy {
		if emailID, ok := id.(string); ok {
			// Find and delete the message
			mailboxes, _ := s.db.ListMailboxes(user)
			var found bool

			for _, mbox := range mailboxes {
				uids, _ := s.db.GetMessageUIDs(user, mbox)
				for _, uid := range uids {
					meta, err := s.db.GetMessageMetadata(user, mbox, uid)
					if err != nil || meta == nil {
						continue
					}
					if meta.MessageID == emailID {
						// Delete message data from store
						_ = s.msgStore.DeleteMessage(user, emailID)
						// Delete metadata from database
						_ = s.db.DeleteMessage(user, mbox, uid)
						found = true
						break
					}
				}
				if found {
					break
				}
			}

			if found {
				destroyed = append(destroyed, emailID)
			} else {
				notDestroyed[emailID] = map[string]interface{}{
					"type": "notFound",
				}
			}
		}
	}

	// RFC 8620 §4.3: newState is the state after the request — the journal
	// sequence now includes this request's changes, which the storage layer
	// recorded via StoreMessageMetadata/DeleteMessage.
	newState, err := s.db.CurrentChangeState(user)
	if err != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("CurrentChangeState", err),
			},
			ID: call.ID,
		}
	}

	return Response{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId":    accountID,
			"oldState":     oldState,
			"newState":     newState,
			"created":      created,
			"updated":      updated,
			"destroyed":    destroyed,
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		},
		ID: call.ID,
	}
}

// mailboxExists reports whether the user's account has a mailbox with this
// name. GetMailbox cannot answer this — it synthesizes a default Mailbox for
// missing buckets — so existence is checked against ListMailboxes.
func (s *Server) mailboxExists(user, name string) bool {
	mailboxes, err := s.db.ListMailboxes(user)
	if err != nil {
		return false
	}
	for _, m := range mailboxes {
		if m == name {
			return true
		}
	}
	return false
}

// stateToken returns the account's change-journal state token (RFC 8620 §2)
// — the same format the */changes methods accept and return — for the state
// fields of /get, /query and /set responses. Read failures degrade to the
// empty-journal token "0", which makes the client's next /changes a safe full
// resync; the failure is logged, mirroring the best-effort RecordChange
// convention in the storage layer.
func (s *Server) stateToken(user string) string {
	token, err := s.db.CurrentChangeState(user)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("CurrentChangeState failed; degrading to empty-journal state", "user", user, "error", err)
		}
		return "0"
	}
	return token
}

// handleEmailImport handles Email/import method
func (s *Server) handleEmailImport(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Email/import", call.ID); !valid {
		return resp
	}

	// RFC 8620 §4.3: /import responses carry the state before (oldState) and
	// after (newState) the request as change-journal tokens (RFC 8620 §2),
	// so clients can resume Email/changes from them.
	oldState, stateErr := s.db.CurrentChangeState(user)
	if stateErr != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("CurrentChangeState", stateErr),
			},
			ID: call.ID,
		}
	}

	emails, _ := args["emails"].(map[string]interface{})

	created := make(map[string]Email)
	notCreated := make(map[string]interface{})

	for key, val := range emails {
		importData, ok := val.(map[string]interface{})
		if !ok {
			notCreated[key] = map[string]interface{}{
				"type":        "invalidArguments",
				"description": "Invalid email import data",
			}
			continue
		}

		blobID, _ := importData["blobId"].(string)
		mailboxIDs, _ := importData["mailboxIds"].(map[string]interface{})
		keywords, _ := importData["keywords"].(map[string]interface{})
		receivedAt, _ := importData["receivedAt"].(string)

		if blobID == "" {
			notCreated[key] = map[string]interface{}{
				"type":        "invalidArguments",
				"description": "blobId is required",
			}
			continue
		}

		// RFC 8621 §4.6: mailboxIds is Id[Boolean] — the email is imported
		// into EVERY mailbox whose value is true, not just the first (the
		// old loop broke after the first entry and silently dropped the
		// rest). Resolve and dedupe names: two ids may map to one name.
		var targetNames []string
		seenNames := make(map[string]bool)
		for id, v := range mailboxIDs {
			if b, ok := v.(bool); ok && b {
				name := getMailboxNameFromID(id)
				if !seenNames[name] {
					seenNames[name] = true
					targetNames = append(targetNames, name)
				}
			}
		}
		if len(targetNames) == 0 {
			targetNames = append(targetNames, "INBOX")
		}

		// RFC 8620 §2.3: mailboxIds reference existing Mailbox objects, and
		// RFC 8621 §4.4 requires the import to fail with "mailboxNotFound"
		// otherwise. getMailboxNameFromID passes unknown ids through
		// verbatim, so without this check the storage layer would silently
		// materialize a phantom mailbox (CreateBucketIfNotExists). The gate
		// covers every target: one missing mailbox rejects the whole entry.
		var missingMbox string
		for _, targetName := range targetNames {
			if !s.mailboxExists(user, targetName) {
				missingMbox = targetName
				break
			}
		}
		if missingMbox != "" {
			notCreated[key] = map[string]interface{}{
				"type":        "mailboxNotFound",
				"description": fmt.Sprintf("Mailbox %s not found", missingMbox),
			}
			continue
		}

		// Retrieve blob data from message store
		// In this implementation, blobID is the message ID
		data, err := s.msgStore.ReadMessage(user, blobID)
		if err != nil {
			notCreated[key] = map[string]interface{}{
				"type":        "blobNotFound",
				"description": fmt.Sprintf("Blob %s not found", blobID),
			}
			continue
		}

		// Parse email headers to extract metadata
		meta := parseEmailMetadata(data, blobID)

		// Set received time if provided
		if receivedAt != "" {
			if t, err := time.Parse(time.RFC3339, receivedAt); err == nil {
				meta.InternalDate = t
			}
		}

		// Convert keywords to flags
		for kw, val := range keywords {
			if b, ok := val.(bool); ok && b {
				switch kw {
				case "$seen":
					meta.Flags = append(meta.Flags, "\\Seen")
				case "$answered":
					meta.Flags = append(meta.Flags, "\\Answered")
				case "$flagged":
					meta.Flags = append(meta.Flags, "\\Flagged")
				case "$draft":
					meta.Flags = append(meta.Flags, "\\Draft")
				}
			}
		}

		// Store the message in EVERY target mailbox, each with its own UID.
		stored := true
		for _, targetName := range targetNames {
			uid, err := s.db.GetNextUID(user, targetName)
			if err != nil {
				notCreated[key] = map[string]interface{}{
					"type":        "serverFail",
					"description": s.safeError("GetNextUID", err),
				}
				stored = false
				break
			}
			meta.UID = uid

			// Store metadata in database
			if err := s.db.StoreMessageMetadata(user, targetName, uid, meta); err != nil {
				notCreated[key] = map[string]interface{}{
					"type":        "serverFail",
					"description": s.safeError("StoreMessageMetadata", err),
				}
				stored = false
				break
			}
		}
		if !stored {
			continue
		}

		// Convert to JMAP Email; the created object reports every mailbox
		// the message was imported into, not just the first.
		email := storageToJMAPEmail(meta, nil, targetNames[0])
		for _, targetName := range targetNames[1:] {
			email.MailboxIDs[getMailboxIDFromName(targetName)] = true
		}
		created[key] = email
	}

	// RFC 8620 §4.3: newState is the state after the request — the journal
	// sequence now includes this import's changes (recorded via
	// StoreMessageMetadata).
	newState, err := s.db.CurrentChangeState(user)
	if err != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("CurrentChangeState", err),
			},
			ID: call.ID,
		}
	}

	return Response{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId":  accountID,
			"oldState":   oldState,
			"newState":   newState,
			"created":    created,
			"notCreated": notCreated,
		},
		ID: call.ID,
	}
}

// parseEmailMetadata extracts metadata from email data
func parseEmailMetadata(data []byte, messageID string) *storage.MessageMetadata {
	meta := &storage.MessageMetadata{
		MessageID:    messageID,
		InternalDate: time.Now(),
		Flags:        []string{},
		Size:         int64(len(data)),
	}

	// Parse headers
	headers := make(map[string]string)
	lines := strings.Split(string(data), "\n")
	inHeaders := true
	var currentHeader string

	for _, line := range lines {
		if inHeaders {
			if line == "\r" || line == "" {
				inHeaders = false
				continue
			}
			// Continuation line
			if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
				if currentHeader != "" {
					headers[currentHeader] += " " + strings.TrimSpace(line)
				}
				continue
			}
			// New header
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				currentHeader = strings.ToLower(strings.TrimSpace(parts[0]))
				headers[currentHeader] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Extract fields
	if subject, ok := headers["subject"]; ok {
		meta.Subject = subject
	}
	if from, ok := headers["from"]; ok {
		meta.From = from
	}
	if to, ok := headers["to"]; ok {
		meta.To = to
	}
	if date, ok := headers["date"]; ok {
		meta.Date = date
	}
	if inReplyTo, ok := headers["in-reply-to"]; ok {
		meta.InReplyTo = inReplyTo
	}

	return meta
}

// handleThreadGet handles Thread/get method
func (s *Server) handleThreadGet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Thread/get", call.ID); !valid {
		return resp
	}

	ids, _ := args["ids"].([]interface{})

	// Get threads from storage
	var threads []Thread
	var notFound []string
	for _, id := range ids {
		if idStr, ok := id.(string); ok {
			// Get thread messages from database
			threadMsgs, err := s.db.GetThreadMessages(user, "INBOX", idStr)
			if err != nil || len(threadMsgs) == 0 {
				// RFC 8620 §4.2: a requested id with no matching thread is
				// reported in notFound instead of returning a phantom empty
				// thread.
				notFound = append(notFound, idStr)
				continue
			}

			var emailIDs []string
			for _, msg := range threadMsgs {
				emailIDs = append(emailIDs, msg.MessageID)
			}

			thread := Thread{
				ID:       idStr,
				EmailIDs: emailIDs,
			}
			threads = append(threads, thread)
		}
	}

	return Response{
		Name: "Thread/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"state":     s.stateToken(user),
			"list":      threads,
			"notFound":  notFound,
		},
		ID: call.ID,
	}
}

// handleSearchSnippetGet handles SearchSnippet/get method
func (s *Server) handleSearchSnippetGet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "SearchSnippet/get", call.ID); !valid {
		return resp
	}

	emailIDs, _ := args["emailIds"].([]interface{})
	search, _ := args["search"].(map[string]interface{})

	// Extract search query text
	searchText := ""
	if text, ok := search["text"].(string); ok {
		searchText = text
	}

	var snippets []SearchSnippet
	var notFound []string

	for _, id := range emailIDs {
		if idStr, ok := id.(string); ok {
			// Try to read the message to generate snippet
			data, err := s.msgStore.ReadMessage(user, idStr)
			if err != nil {
				notFound = append(notFound, idStr)
				continue
			}

			snippet := s.generateSearchSnippet(string(data), searchText)
			snippet.EmailID = idStr
			snippets = append(snippets, snippet)
		}
	}

	return Response{
		Name: "SearchSnippet/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"list":      snippets,
			"notFound":  notFound,
		},
		ID: call.ID,
	}
}

// generateSearchSnippet generates a search snippet from email content
func (s *Server) generateSearchSnippet(emailData, searchText string) SearchSnippet {
	lines := strings.Split(emailData, "\n")
	// RFC 5322 emails use CRLF; after splitting on "\n" each line keeps a
	// trailing "\r", which would defeat the empty-line header/body
	// delimiter (a CRLF blank line is "\r", not "") and leak CRs into the
	// preview. Normalize once, here.
	for i, line := range lines {
		lines[i] = strings.TrimSuffix(line, "\r")
	}
	var subject string
	var bodyLines []string
	inBody := false

	for _, line := range lines {
		// Stop at empty line after headers (body starts)
		if !inBody && line == "" {
			inBody = true
			continue
		}

		if !inBody {
			// Parse headers
			if strings.HasPrefix(strings.ToLower(line), "subject:") {
				// The prefix match is case-insensitive and ToLower preserves
				// byte length, so slice the original line at the prefix
				// length — "SUBJECT:"/"Subject:" strip identically.
				subject = strings.TrimSpace(line[len("subject:"):])
			}
		} else {
			// Collect body
			bodyLines = append(bodyLines, line)
		}
	}

	body := strings.TrimSpace(strings.Join(bodyLines, " "))

	// RFC 8621 §5.3: the preview SHOULD contain the part of the body where
	// the search match occurred. Anchor the window at the first
	// case-insensitive occurrence of searchText; without search text or on
	// no match, fall back to the leading body text. (Matching itself is the
	// client's query job — the ids arrive pre-selected.)
	if needle := strings.TrimSpace(searchText); needle != "" {
		if idx := strings.Index(strings.ToLower(body), strings.ToLower(needle)); idx >= 0 {
			body = body[idx:]
		}
	}

	if len(body) > 150 {
		body = body[:150] + "..."
	}

	return SearchSnippet{
		Subject: subject,
		Preview: body,
	}
}

// handleIdentityGet handles Identity/get method
func (s *Server) handleIdentityGet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Identity/get", call.ID); !valid {
		return resp
	}

	ids, _ := args["ids"].([]interface{})

	// Get user info from database
	// For now, return a default identity
	identities := []Identity{
		{
			ID:            "default",
			Name:          user,
			Email:         user,
			ReplyTo:       nil,
			Bcc:           nil,
			TextSignature: "",
			HTMLSignature: "",
			MayDelete:     false,
		},
	}

	// Filter by IDs if specified
	var result []Identity
	var notFound []string
	if len(ids) > 0 {
		idSet := make(map[string]bool)
		for _, id := range ids {
			if str, ok := id.(string); ok {
				idSet[str] = true
			}
		}
		foundIDs := make(map[string]bool)
		for _, identity := range identities {
			if idSet[identity.ID] {
				result = append(result, identity)
				foundIDs[identity.ID] = true
			}
		}
		// RFC 8620 §4.2: requested ids with no matching record are reported
		// in notFound rather than silently dropped.
		for id := range idSet {
			if !foundIDs[id] {
				notFound = append(notFound, id)
			}
		}
	} else {
		result = identities
	}

	return Response{
		Name: "Identity/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"state":     s.stateToken(user),
			"list":      result,
			"notFound":  notFound,
		},
		ID: call.ID,
	}
}

// handleIdentitySet handles Identity/set method
func (s *Server) handleIdentitySet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Validate accountId matches authenticated user
	if valid, resp := validateAccountId(accountID, user, "Identity/set", call.ID); !valid {
		return resp
	}

	// Parse create, update, destroy
	create, _ := args["create"].(map[string]interface{})
	update, _ := args["update"].(map[string]interface{})
	destroy, _ := args["destroy"].([]interface{})

	// Identity is currently read-only - derived from account settings
	// Return notSupported error for any write operations

	notCreated := make(map[string]interface{})
	for id := range create {
		notCreated[id] = map[string]interface{}{
			"type":    "notSupported",
			"message": "Identity creation is not supported. Identities are derived from account settings.",
		}
	}

	notUpdated := make(map[string]interface{})
	for id := range update {
		notUpdated[id] = map[string]interface{}{
			"type":    "notSupported",
			"message": "Identity modification is not supported. Identities are derived from account settings.",
		}
	}

	notDestroyed := make(map[string]interface{})
	for _, id := range destroy {
		// Identities are read-only (derived from account settings), so every
		// destroy is rejected — and RFC 8620 §5.3 requires EVERY requested id
		// to be reported in destroyed or notDestroyed. The old code reported
		// only the literal "default" id and dropped all others from the
		// response entirely.
		if idStr, ok := id.(string); ok {
			notDestroyed[idStr] = map[string]interface{}{
				"type":    "notSupported",
				"message": "Identity deletion is not supported. Identities are derived from account settings.",
			}
		}
	}

	return Response{
		Name: "Identity/set",
		Args: map[string]interface{}{
			"accountId":    accountID,
			"oldState":     nil,
			"newState":     s.stateToken(user),
			"created":      map[string]interface{}{},
			"updated":      map[string]interface{}{},
			"destroyed":    []string{},
			"notCreated":   notCreated,
			"notUpdated":   notUpdated,
			"notDestroyed": notDestroyed,
		},
		ID: call.ID,
	}
}

// Helper functions

func parseFilter(filter interface{}) *FilterCondition {
	if filter == nil {
		return nil
	}

	// Parse filter from JSON
	data, _ := json.Marshal(filter)
	var condition FilterCondition
	_ = json.Unmarshal(data, &condition)
	return &condition
}

// getMailboxIDFromName converts a mailbox name to JMAP ID
func getMailboxIDFromName(name string) string {
	switch name {
	case "INBOX":
		return "inbox"
	case "Sent":
		return "sent"
	case "Drafts":
		return "drafts"
	case "Trash":
		return "trash"
	case "Junk":
		return "junk"
	case "Archive":
		return "archive"
	default:
		return name
	}
}

// getMailboxNameFromID converts a JMAP mailbox ID to name
func getMailboxNameFromID(id string) string {
	switch id {
	case "inbox":
		return "INBOX"
	case "sent":
		return "Sent"
	case "drafts":
		return "Drafts"
	case "trash":
		return "Trash"
	case "junk":
		return "Junk"
	case "archive":
		return "Archive"
	default:
		return id
	}
}

// handleMailboxChanges handles Mailbox/changes method (RFC 8620)
func (s *Server) handleMailboxChanges(user string, call MethodCall) Response {
	return s.handleChanges(user, call, "Mailbox/changes", storage.ChangeTypeMailbox, true)
}

// handleEmailChanges handles Email/changes method (RFC 8621)
func (s *Server) handleEmailChanges(user string, call MethodCall) Response {
	return s.handleChanges(user, call, "Email/changes", storage.ChangeTypeEmail, false)
}

// handleChanges is the shared implementation backing the */changes methods.
// It reads entries from the per-user change journal and folds them into
// JMAP's created/updated/destroyed sets, deduplicating per ID.
func (s *Server) handleChanges(user string, call MethodCall, methodName string, ct storage.ChangeType, includeUpdatedProps bool) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	sinceState, _ := args["sinceState"].(string)
	maxChanges, _ := args["maxChanges"].(float64)

	if valid, resp := validateAccountId(accountID, user, methodName, call.ID); !valid {
		return resp
	}

	if maxChanges == 0 || maxChanges > 256 {
		maxChanges = 256
	}

	since := storage.ParseChangeState(sinceState)
	entries, hasMore, lastSeq, err := s.db.GetChangesSince(user, ct, since, int(maxChanges))
	if err != nil {
		return Response{
			Name: methodName,
			Args: map[string]interface{}{
				"type":        "serverFail",
				"description": s.safeError("GetChangesSince", err),
			},
			ID: call.ID,
		}
	}

	// Reduce to one entry per ID; destruction wins over updates, and creates
	// followed by destruction in the SAME mailbox collapse to nothing (per
	// JMAP semantics). A create and destroy in DIFFERENT mailboxes is a move:
	// the object's mailboxIds changed, so it must surface as updated —
	// otherwise a set-made move folds to nothing and syncing clients never
	// learn the message moved.
	type fold struct {
		created, updated, destroyed bool
		createdMbox, destroyedMbox  string
	}
	state := make(map[string]*fold)
	order := []string{}
	for _, e := range entries {
		f, ok := state[e.ID]
		if !ok {
			f = &fold{}
			state[e.ID] = f
			order = append(order, e.ID)
		}
		switch e.Kind {
		case storage.ChangeKindCreated:
			f.created = true
			f.createdMbox = e.Mailbox
		case storage.ChangeKindUpdated:
			f.updated = true
		case storage.ChangeKindDestroyed:
			f.destroyed = true
			f.destroyedMbox = e.Mailbox
		}
	}

	created := make([]string, 0)
	updated := make([]string, 0)
	destroyed := make([]string, 0)
	for _, id := range order {
		f := state[id]
		moved := f.created && f.destroyed && f.createdMbox != f.destroyedMbox
		switch {
		case f.created && f.destroyed && !moved:
			// no-op: the object never existed within this window
		case moved:
			updated = append(updated, id)
		case f.destroyed:
			destroyed = append(destroyed, id)
		case f.created:
			created = append(created, id)
		case f.updated:
			updated = append(updated, id)
		}
	}

	out := map[string]interface{}{
		"accountId":      accountID,
		"oldState":       sinceState,
		"newState":       fmt.Sprintf("%d", lastSeq),
		"hasMoreChanges": hasMore,
		"created":        created,
		"updated":        updated,
		"destroyed":      destroyed,
	}
	if includeUpdatedProps {
		out["updatedProperties"] = nil
	}

	return Response{
		Name: methodName,
		Args: out,
		ID:   call.ID,
	}
}

// handleMailboxQueryChanges handles Mailbox/queryChanges method (RFC 8620)
func (s *Server) handleMailboxQueryChanges(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	sinceQueryState, _ := args["sinceQueryState"].(string)

	if valid, resp := validateAccountId(accountID, user, "Mailbox/queryChanges", call.ID); !valid {
		return resp
	}

	// RFC 8620 §5.6: deltas are computed by diffing the stored snapshot of
	// the query result (saved by Mailbox/query) against a fresh run. Without
	// a matching snapshot the delta cannot be computed and the server MUST
	// answer stateMismatch.
	snap, err := s.db.GetQuerySnapshot(user, querySnapshotHash("Mailbox/query", nil, nil))
	if err != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}
	if sinceQueryState == "" || storage.ParseChangeState(sinceQueryState) != snap.Seq {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}

	newIDs := s.runMailboxQuery(user)
	newSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newSet[id] = true
	}
	oldSet := make(map[string]bool, len(snap.IDs))
	for _, id := range snap.IDs {
		oldSet[id] = true
	}

	added := []map[string]interface{}{}
	removed := []int{}
	for i, id := range snap.IDs {
		if !newSet[id] {
			removed = append(removed, i)
		}
	}
	for j, id := range newIDs {
		if !oldSet[id] {
			added = append(added, map[string]interface{}{"index": j, "id": id})
		}
	}

	return Response{
		Name: "Mailbox/queryChanges",
		Args: map[string]interface{}{
			"accountId":      accountID,
			"oldQueryState":  sinceQueryState,
			"newQueryState":  s.stateToken(user),
			"hasMoreChanges": false,
			"added":          added,
			"removed":        removed,
			"total":          len(newIDs),
		},
		ID: call.ID,
	}
}

// handleEmailQueryChanges handles Email/queryChanges method (RFC 8620)
func (s *Server) handleEmailQueryChanges(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	sinceQueryState, _ := args["sinceQueryState"].(string)
	sortList, _ := args["sort"].([]interface{})

	if valid, resp := validateAccountId(accountID, user, "Email/queryChanges", call.ID); !valid {
		return resp
	}

	// RFC 8620 §5.6: deltas are computed by diffing the stored snapshot of
	// the query result (saved by Email/query) against a fresh run. Without a
	// matching snapshot the delta cannot be computed and the server MUST
	// answer stateMismatch.
	snap, err := s.db.GetQuerySnapshot(user, querySnapshotHash("Email/query", args["filter"], sortList))
	if err != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}
	if sinceQueryState == "" || storage.ParseChangeState(sinceQueryState) != snap.Seq {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}

	newIDs := s.runEmailQuery(user, args["filter"], sortList)
	newSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newSet[id] = true
	}
	oldSet := make(map[string]bool, len(snap.IDs))
	for _, id := range snap.IDs {
		oldSet[id] = true
	}

	added := []map[string]interface{}{}
	removed := []int{}
	for i, id := range snap.IDs {
		if !newSet[id] {
			removed = append(removed, i)
		}
	}
	for j, id := range newIDs {
		if !oldSet[id] {
			added = append(added, map[string]interface{}{"index": j, "id": id})
		}
	}

	return Response{
		Name: "Email/queryChanges",
		Args: map[string]interface{}{
			"accountId":      accountID,
			"oldQueryState":  sinceQueryState,
			"newQueryState":  s.stateToken(user),
			"hasMoreChanges": false,
			"added":          added,
			"removed":        removed,
			"total":          len(newIDs),
		},
		ID: call.ID,
	}
}

// handleThreadQuery handles Thread/query method (RFC 8620)
func (s *Server) handleThreadQuery(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	if valid, resp := validateAccountId(accountID, user, "Thread/query", call.ID); !valid {
		return resp
	}

	filter := args["filter"]
	sortList, _ := args["sort"].([]interface{})
	position, _ := args["position"].(float64)
	limit, _ := args["limit"].(float64)

	if limit == 0 || limit > 100 {
		limit = 30
	}

	threadIDs := s.runThreadQuery(user, filter, sortList)

	// Snapshot the full ordered result for Thread/queryChanges deltas
	// (RFC 8620 §5.6), stamped with the current state sequence.
	queryState := s.stateToken(user)
	_ = s.db.SaveQuerySnapshot(user, &storage.QuerySnapshot{
		QueryHash: querySnapshotHash("Thread/query", filter, sortList),
		Seq:       storage.ParseChangeState(queryState),
		IDs:       threadIDs,
	})

	total := len(threadIDs)
	// position is a client-supplied offset and may arrive negative. Clamping
	// only the upper bound would leave start negative and make
	// threadIDs[start:end] a slice-bounds panic, so floor it at 0 first.
	start := int(position)
	if start < 0 {
		start = 0
	}
	if start > total {
		start = total
	}
	end := start + int(limit)
	if end > total {
		end = total
	}

	ids := threadIDs[start:end]

	return Response{
		Name: "Thread/query",
		Args: map[string]interface{}{
			"accountId":           accountID,
			"queryState":          queryState,
			"canCalculateChanges": false,
			"position":            int(position),
			"total":               total,
			"ids":                 ids,
		},
		ID: call.ID,
	}
}

// runThreadQuery computes the full ordered result of a Thread/query — the
// distinct thread IDs of all matching messages — independent of paging;
// shared by Thread/query and the Thread/queryChanges delta computation.
func (s *Server) runThreadQuery(user string, filter interface{}, sortArg interface{}) []string {
	filterCondition := parseFilter(filter)
	sortList, _ := sortArg.([]interface{})

	var threadIDs []string
	threadSet := make(map[string]bool)

	mailboxes, _ := s.db.ListMailboxes(user)
	for _, mbox := range mailboxes {
		uids, _ := s.db.GetMessageUIDs(user, mbox)
		for _, uid := range uids {
			meta, err := s.db.GetMessageMetadata(user, mbox, uid)
			if err != nil || meta == nil {
				continue
			}

			if !matchesFilter(meta, filterCondition) {
				continue
			}

			if meta.ThreadID != "" && !threadSet[meta.ThreadID] {
				threadSet[meta.ThreadID] = true
				threadIDs = append(threadIDs, meta.ThreadID)
			}
		}
	}

	// Sort threads (by most recent message in thread)
	if len(sortList) > 0 {
		data, _ := json.Marshal(sortList[0])
		var comp Comparator
		_ = json.Unmarshal(data, &comp)
		_ = comp
		// Sort by thread's most recent message
		sort.Slice(threadIDs, func(i, j int) bool {
			return threadIDs[i] < threadIDs[j]
		})
	}

	return threadIDs
}

// handleThreadChanges handles Thread/changes method (RFC 8620)
func (s *Server) handleThreadChanges(user string, call MethodCall) Response {
	return s.handleChanges(user, call, "Thread/changes", storage.ChangeTypeThread, false)
}

// handleThreadQueryChanges handles Thread/queryChanges method (RFC 8620)
func (s *Server) handleThreadQueryChanges(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	sinceQueryState, _ := args["sinceQueryState"].(string)

	if valid, resp := validateAccountId(accountID, user, "Thread/queryChanges", call.ID); !valid {
		return resp
	}

	// RFC 8620 §5.6: deltas are computed by diffing the stored snapshot of
	// the query result (saved by Thread/query) against a fresh run. Without
	// a matching snapshot the delta cannot be computed and the server MUST
	// answer stateMismatch.
	snap, err := s.db.GetQuerySnapshot(user, querySnapshotHash("Thread/query", args["filter"], args["sort"]))
	if err != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}
	if sinceQueryState == "" || storage.ParseChangeState(sinceQueryState) != snap.Seq {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}

	newIDs := s.runThreadQuery(user, args["filter"], args["sort"])
	newSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newSet[id] = true
	}
	oldSet := make(map[string]bool, len(snap.IDs))
	for _, id := range snap.IDs {
		oldSet[id] = true
	}

	added := []map[string]interface{}{}
	removed := []int{}
	for i, id := range snap.IDs {
		if !newSet[id] {
			removed = append(removed, i)
		}
	}
	for j, id := range newIDs {
		if !oldSet[id] {
			added = append(added, map[string]interface{}{"index": j, "id": id})
		}
	}

	return Response{
		Name: "Thread/queryChanges",
		Args: map[string]interface{}{
			"accountId":      accountID,
			"oldQueryState":  sinceQueryState,
			"newQueryState":  s.stateToken(user),
			"hasMoreChanges": false,
			"added":          added,
			"removed":        removed,
			"total":          len(newIDs),
		},
		ID: call.ID,
	}
}

// handleIdentityChanges handles Identity/changes method (RFC 8620)
func (s *Server) handleIdentityChanges(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	sinceState, _ := args["sinceState"].(string)
	maxChanges, _ := args["maxChanges"].(float64)

	if valid, resp := validateAccountId(accountID, user, "Identity/changes", call.ID); !valid {
		return resp
	}

	if maxChanges == 0 || maxChanges > 256 {
		maxChanges = 256
	}

	_ = sinceState

	// Identities are derived from account settings - rarely change
	return Response{
		Name: "Identity/changes",
		Args: map[string]interface{}{
			"accountId":      accountID,
			"oldState":       sinceState,
			"newState":       s.stateToken(user),
			"hasMoreChanges": false,
			"created":        []string{},
			"updated":        []string{},
			"destroyed":      []string{},
		},
		ID: call.ID,
	}
}

// handleIdentityQuery handles Identity/query method (RFC 8620)
func (s *Server) handleIdentityQuery(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	if valid, resp := validateAccountId(accountID, user, "Identity/query", call.ID); !valid {
		return resp
	}

	position, _ := args["position"].(float64)
	limit, _ := args["limit"].(float64)
	calculateTotal, _ := args["calculateTotal"].(bool)

	if limit == 0 || limit > 256 {
		limit = 256
	}

	// Identities are read-only and derived from account settings: the result
	// is the single default identity (handleIdentitySet rejects all writes).
	ids := s.runIdentityQuery(user)

	// Snapshot for Identity/queryChanges deltas (RFC 8620 §5.6).
	queryState := s.stateToken(user)
	_ = s.db.SaveQuerySnapshot(user, &storage.QuerySnapshot{
		QueryHash: querySnapshotHash("Identity/query", nil, nil),
		Seq:       storage.ParseChangeState(queryState),
		IDs:       ids,
	})

	total := len(ids)
	if !calculateTotal {
		total = 0
	}

	return Response{
		Name: "Identity/query",
		Args: map[string]interface{}{
			"accountId":           accountID,
			"queryState":          queryState,
			"canCalculateChanges": false,
			"position":            int(position),
			"total":               total,
			"ids":                 ids,
		},
		ID: call.ID,
	}
}

// runIdentityQuery computes the full ordered result of an Identity/query.
// Identities are read-only and derived from account settings: the list is
// the single default identity, so the result is invariant.
func (s *Server) runIdentityQuery(user string) []string {
	return []string{"default"}
}

// handleIdentityQueryChanges handles Identity/queryChanges method (RFC 8620)
func (s *Server) handleIdentityQueryChanges(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
	sinceQueryState, _ := args["sinceQueryState"].(string)

	if valid, resp := validateAccountId(accountID, user, "Identity/queryChanges", call.ID); !valid {
		return resp
	}

	// RFC 8620 §5.6: deltas are computed by diffing the stored snapshot of
	// the query result (saved by Identity/query) against a fresh run — for
	// the read-only identity model the diff is always empty. Without a
	// matching snapshot the delta cannot be computed and the server MUST
	// answer stateMismatch.
	snap, err := s.db.GetQuerySnapshot(user, querySnapshotHash("Identity/query", nil, nil))
	if err != nil {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}
	if sinceQueryState == "" || storage.ParseChangeState(sinceQueryState) != snap.Seq {
		return Response{
			Name: "error",
			Args: map[string]interface{}{"type": "stateMismatch"},
			ID:   call.ID,
		}
	}

	newIDs := s.runIdentityQuery(user)
	newSet := make(map[string]bool, len(newIDs))
	for _, id := range newIDs {
		newSet[id] = true
	}
	oldSet := make(map[string]bool, len(snap.IDs))
	for _, id := range snap.IDs {
		oldSet[id] = true
	}

	added := []map[string]interface{}{}
	removed := []int{}
	for i, id := range snap.IDs {
		if !newSet[id] {
			removed = append(removed, i)
		}
	}
	for j, id := range newIDs {
		if !oldSet[id] {
			added = append(added, map[string]interface{}{"index": j, "id": id})
		}
	}

	return Response{
		Name: "Identity/queryChanges",
		Args: map[string]interface{}{
			"accountId":      accountID,
			"oldQueryState":  sinceQueryState,
			"newQueryState":  s.stateToken(user),
			"hasMoreChanges": false,
			"added":          added,
			"removed":        removed,
			"total":          len(newIDs),
		},
		ID: call.ID,
	}
}

// storageToJMAPEmail converts storage metadata to JMAP Email
func storageToJMAPEmail(meta *storage.MessageMetadata, properties []string, mailbox string) Email {
	mailboxID := getMailboxIDFromName(mailbox)
	email := Email{
		ID:         meta.MessageID,
		BlobID:     meta.MessageID,
		ThreadID:   meta.ThreadID,
		MailboxIDs: map[string]bool{mailboxID: true},
		Keywords:   make(map[string]bool),
		Size:       meta.Size,
		ReceivedAt: meta.InternalDate.Format(time.RFC3339),
		Subject:    meta.Subject,
		From:       []EmailAddress{{Email: meta.From}},
		To:         []EmailAddress{{Email: meta.To}},
	}

	// Convert flags to keywords
	for _, flag := range meta.Flags {
		switch flag {
		case "\\Seen":
			email.Keywords["$seen"] = true
		case "\\Answered":
			email.Keywords["$answered"] = true
		case "\\Flagged":
			email.Keywords["$flagged"] = true
		case "\\Draft":
			email.Keywords["$draft"] = true
		}
	}

	return email
}
