package jmap

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// handleMailboxGet handles Mailbox/get method
func (s *Server) handleMailboxGet(user string, call MethodCall) Response {
	args := call.Args

	accountID, _ := args["accountId"].(string)
	ids, _ := args["ids"].([]interface{})

	// Get mailboxes from storage
	mailboxNames, err := s.db.ListMailboxes(user)
	if err != nil {
		return Response{
			Name: "Mailbox/get",
			Args: map[string]interface{}{
				"accountId": accountID,
				"state":     fmt.Sprintf("state-%d", time.Now().Unix()),
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
	if len(ids) > 0 {
		idSet := make(map[string]bool)
		for _, id := range ids {
			if str, ok := id.(string); ok {
				idSet[str] = true
			}
		}
		for _, mbox := range mailboxes {
			if idSet[mbox.ID] {
				result = append(result, mbox)
			}
		}
	} else {
		result = mailboxes
	}

	return Response{
		Name: "Mailbox/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"state":     fmt.Sprintf("state-%d", time.Now().Unix()),
			"list":      result,
			"notFound":  []string{},
		},
		ID: call.ID,
	}
}

// handleMailboxQuery handles Mailbox/query method
func (s *Server) handleMailboxQuery(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

	// Get mailboxes from storage
	mailboxNames, err := s.db.ListMailboxes(user)
	if err != nil {
		return Response{
			Name: "Mailbox/query",
			Args: map[string]interface{}{
				"accountId":           accountID,
				"queryState":          fmt.Sprintf("state-%d", time.Now().Unix()),
				"canCalculateChanges": false,
				"position":            0,
				"total":               0,
				"ids":                 []string{},
			},
			ID: call.ID,
		}
	}

	var ids []string
	for _, name := range mailboxNames {
		ids = append(ids, getMailboxIDFromName(name))
	}

	return Response{
		Name: "Mailbox/query",
		Args: map[string]interface{}{
			"accountId":           accountID,
			"queryState":          fmt.Sprintf("state-%d", time.Now().Unix()),
			"canCalculateChanges": false,
			"position":            0,
			"total":               len(ids),
			"ids":                 ids,
		},
		ID: call.ID,
	}
}

// handleMailboxSet handles Mailbox/set method
func (s *Server) handleMailboxSet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

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
				"description": err.Error(),
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
					"description": err.Error(),
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
					"description": err.Error(),
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
			"newState":     fmt.Sprintf("state-%d", time.Now().Unix()),
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
			"state":     fmt.Sprintf("state-%d", time.Now().Unix()),
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
	filter, _ := args["filter"]
	sort, _ := args["sort"].([]interface{})
	position, _ := args["position"].(float64)
	limit, _ := args["limit"].(float64)

	// Default limit
	if limit == 0 || limit > 100 {
		limit = 30
	}

	// Parse filter
	filterCondition := parseFilter(filter)

	// Get all messages from mailboxes
	var allMessages []struct {
		id      string
		mailbox string
		uid     uint32
		meta    *storage.MessageMetadata
	}

	mailboxes, _ := s.db.ListMailboxes(user)
	targetMbox := ""

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
	if len(sort) > 0 {
		// Parse first sort comparator
		data, _ := json.Marshal(sort[0])
		var comp Comparator
		json.Unmarshal(data, &comp)

		// Sort messages
		sortMessages(allMessages, comp)
	}

	// Apply position and limit
	total := len(allMessages)
	start := int(position)
	if start > total {
		start = total
	}
	end := start + int(limit)
	if end > total {
		end = total
	}

	var ids []string
	for i := start; i < end; i++ {
		ids = append(ids, allMessages[i].id)
	}

	return Response{
		Name: "Email/query",
		Args: map[string]interface{}{
			"accountId":           accountID,
			"queryState":          fmt.Sprintf("state-%d", time.Now().Unix()),
			"canCalculateChanges": false,
			"position":            int(position),
			"total":               total,
			"ids":                 ids,
		},
		ID: call.ID,
	}
}

// matchesFilter checks if a message matches the filter condition
func matchesFilter(meta *storage.MessageMetadata, filter *FilterCondition) bool {
	if filter == nil {
		return true
	}

	// Filter by inMailbox
	if filter.InMailbox != "" {
		// Already filtered at mailbox level
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

	ascending := comp.IsAscending
	if comp.Property == "receivedAt" || comp.Property == "sentAt" {
		ascending = !ascending // Default is descending for dates
	}

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

// handleEmailSet handles Email/set method
func (s *Server) handleEmailSet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

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

		// Update keywords (flags)
		if keywords, ok := updateData["keywords"].(map[string]interface{}); ok {
			// Convert JMAP keywords to IMAP flags
			newFlags := []string{}
			for kw, val := range keywords {
				if b, ok := val.(bool); ok && b {
					switch kw {
					case "$seen":
						newFlags = append(newFlags, "\\Seen")
					case "$answered":
						newFlags = append(newFlags, "\\Answered")
					case "$flagged":
						newFlags = append(newFlags, "\\Flagged")
					case "$draft":
						newFlags = append(newFlags, "\\Draft")
					}
				}
			}
			meta.Flags = newFlags
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
				// Move message to new mailbox
				// Store in new mailbox
				newUID, _ := s.db.GetNextUID(user, newMbox)
				if err := s.db.StoreMessageMetadata(user, newMbox, newUID, meta); err != nil {
					notUpdated[emailID] = map[string]interface{}{
						"type":        "serverFail",
						"description": err.Error(),
					}
					continue
				}

				// Delete from old mailbox
				s.db.DeleteMessage(user, targetMbox, targetUID)
				targetMbox = newMbox
			}
		}

		// Save updated metadata
		if err := s.db.UpdateMessageMetadata(user, targetMbox, targetUID, meta); err != nil {
			notUpdated[emailID] = map[string]interface{}{
				"type":        "serverFail",
				"description": err.Error(),
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
						s.msgStore.DeleteMessage(user, emailID)
						// Delete metadata from database
						s.db.DeleteMessage(user, mbox, uid)
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

	return Response{
		Name: "Email/set",
		Args: map[string]interface{}{
			"accountId":    accountID,
			"oldState":     nil,
			"newState":     fmt.Sprintf("state-%d", time.Now().Unix()),
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

// handleEmailImport handles Email/import method
func (s *Server) handleEmailImport(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

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

		// Determine target mailbox
		var targetMbox string
		mboxIDs := make(map[string]bool)
		for id, v := range mailboxIDs {
			if b, ok := v.(bool); ok && b {
				targetMbox = getMailboxNameFromID(id)
				mboxIDs[id] = true
				break
			}
		}
		if targetMbox == "" {
			targetMbox = "INBOX"
			mboxIDs["inbox"] = true
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

		// Get next UID for the mailbox
		uid, err := s.db.GetNextUID(user, targetMbox)
		if err != nil {
			notCreated[key] = map[string]interface{}{
				"type":        "serverFail",
				"description": err.Error(),
			}
			continue
		}
		meta.UID = uid

		// Store metadata in database
		if err := s.db.StoreMessageMetadata(user, targetMbox, uid, meta); err != nil {
			notCreated[key] = map[string]interface{}{
				"type":        "serverFail",
				"description": err.Error(),
			}
			continue
		}

		// Convert to JMAP Email
		email := storageToJMAPEmail(meta, nil, targetMbox)
		created[key] = email
	}

	return Response{
		Name: "Email/import",
		Args: map[string]interface{}{
			"accountId":  accountID,
			"oldState":   nil,
			"newState":   fmt.Sprintf("state-%d", time.Now().Unix()),
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
	ids, _ := args["ids"].([]interface{})

	// Get threads from storage
	var threads []Thread
	for _, id := range ids {
		if idStr, ok := id.(string); ok {
			// Get thread messages from database
			threadMsgs, err := s.db.GetThreadMessages(user, "INBOX", idStr)
			if err != nil {
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
			"state":     fmt.Sprintf("state-%d", time.Now().Unix()),
			"list":      threads,
			"notFound":  []string{},
		},
		ID: call.ID,
	}
}

// handleSearchSnippetGet handles SearchSnippet/get method
func (s *Server) handleSearchSnippetGet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)
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
	var subject, body string
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
				subject = strings.TrimPrefix(line, "subject:")
				subject = strings.TrimSpace(subject)
			}
		} else {
			// Collect body
			if len(body) < 200 {
				body += line + " "
			}
		}
	}

	body = strings.TrimSpace(body)
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
	if len(ids) > 0 {
		idSet := make(map[string]bool)
		for _, id := range ids {
			if str, ok := id.(string); ok {
				idSet[str] = true
			}
		}
		for _, identity := range identities {
			if idSet[identity.ID] {
				result = append(result, identity)
			}
		}
	} else {
		result = identities
	}

	return Response{
		Name: "Identity/get",
		Args: map[string]interface{}{
			"accountId": accountID,
			"state":     fmt.Sprintf("state-%d", time.Now().Unix()),
			"list":      result,
			"notFound":  []string{},
		},
		ID: call.ID,
	}
}

// handleIdentitySet handles Identity/set method
func (s *Server) handleIdentitySet(user string, call MethodCall) Response {
	args := call.Args
	accountID, _ := args["accountId"].(string)

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
		if idStr, ok := id.(string); ok {
			// Allow deleting only if it's not the default identity
			if idStr == "default" {
				notDestroyed[idStr] = map[string]interface{}{
					"type":    "notSupported",
					"message": "Cannot delete the default identity.",
				}
			}
		}
	}

	return Response{
		Name: "Identity/set",
		Args: map[string]interface{}{
			"accountId":    accountID,
			"oldState":     nil,
			"newState":     fmt.Sprintf("state-%d", time.Now().Unix()),
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
	json.Unmarshal(data, &condition)
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
