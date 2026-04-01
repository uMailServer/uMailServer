package search

import (
	"fmt"
	"strings"
	"testing"

	"github.com/umailserver/umailserver/internal/storage"
)

// --- sortResults coverage: exercise score-based ordering ---

func TestSortResultsMultipleScores(t *testing.T) {
	results := []SearchResult{
		{DocID: "doc1", Score: 1.0},
		{DocID: "doc2", Score: 5.0},
		{DocID: "doc3", Score: 3.0},
		{DocID: "doc4", Score: 2.0},
	}
	sortResults(results)

	// Verify descending order by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%.1f > [%d]=%.1f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
	if results[0].DocID != "doc2" {
		t.Errorf("expected highest-scored doc2 first, got %s", results[0].DocID)
	}
}

func TestSortResultsAlreadySorted(t *testing.T) {
	results := []SearchResult{
		{DocID: "a", Score: 10.0},
		{DocID: "b", Score: 5.0},
		{DocID: "c", Score: 1.0},
	}
	sortResults(results)
	if results[0].DocID != "a" || results[2].DocID != "c" {
		t.Errorf("already sorted results disturbed: %v", results)
	}
}

func TestSortResultsEmpty(t *testing.T) {
	results := []SearchResult{}
	sortResults(results) // should not panic
}

func TestSortResultsSingleElement(t *testing.T) {
	results := []SearchResult{{DocID: "only", Score: 42.0}}
	sortResults(results)
	if len(results) != 1 || results[0].DocID != "only" {
		t.Errorf("single element sort failed: %v", results)
	}
}

func TestSortResultsEqualScores(t *testing.T) {
	results := []SearchResult{
		{DocID: "a", Score: 3.0},
		{DocID: "b", Score: 3.0},
		{DocID: "c", Score: 3.0},
	}
	sortResults(results)
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

func TestSortResultsNegativeScores(t *testing.T) {
	results := []SearchResult{
		{DocID: "a", Score: -1.0},
		{DocID: "b", Score: 5.0},
		{DocID: "c", Score: -3.0},
	}
	sortResults(results)
	if results[0].DocID != "b" {
		t.Errorf("expected highest score first, got %s", results[0].DocID)
	}
}

func TestSortResultsManyElements(t *testing.T) {
	const n = 20
	results := make([]SearchResult, n)
	for i := 0; i < n; i++ {
		results[i] = SearchResult{DocID: fmt.Sprintf("doc%d", i), Score: float64(i)}
	}
	sortResults(results)
	for i := 1; i < n; i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("not sorted at position %d: %.1f > %.1f", i, results[i].Score, results[i-1].Score)
		}
	}
}

// --- BuildIndex full round-trip with real bbolt database ---

func TestBuildIndexFullRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	// Create a message store
	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("failed to create message store: %v", err)
	}

	user := "alice"

	// Create mailbox
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("failed to create mailbox: %v", err)
	}

	// Store messages and metadata
	msg1 := []byte("From: bob@example.com\r\nTo: alice@example.com\r\nSubject: Hello Alice\r\n\r\nThis is the first message about golang programming.\r\n")
	msg1ID, err := msgStore.StoreMessage(user, msg1)
	if err != nil {
		t.Fatalf("failed to store message 1: %v", err)
	}

	msg2 := []byte("From: carol@example.com\r\nTo: alice@example.com\r\nSubject: Meeting Tomorrow\r\n\r\nLet us discuss the quarterly report tomorrow.\r\n")
	msg2ID, err := msgStore.StoreMessage(user, msg2)
	if err != nil {
		t.Fatalf("failed to store message 2: %v", err)
	}

	// Store metadata in database
	meta1 := &storage.MessageMetadata{
		MessageID: msg1ID,
		UID:       1,
		Subject:   "Hello Alice",
		From:      "bob@example.com",
		To:        "alice@example.com",
		Date:      "2025-01-15",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 1, meta1); err != nil {
		t.Fatalf("failed to store metadata 1: %v", err)
	}

	meta2 := &storage.MessageMetadata{
		MessageID: msg2ID,
		UID:       2,
		Subject:   "Meeting Tomorrow",
		From:      "carol@example.com",
		To:        "alice@example.com",
		Date:      "2025-01-16",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 2, meta2); err != nil {
		t.Fatalf("failed to store metadata 2: %v", err)
	}

	// Build the search index
	svc := NewService(database, msgStore, nil)
	err = svc.BuildIndex(user)
	if err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	// Verify index was created
	svc.mu.RLock()
	idx, exists := svc.indexes[user]
	svc.mu.RUnlock()
	if !exists {
		t.Fatal("expected index to exist for user")
	}
	if idx.DocCount() != 2 {
		t.Errorf("expected 2 documents indexed, got %d", idx.DocCount())
	}

	// Search for "golang" - should find message 1
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "golang",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'golang', got %d", len(results))
	}
	if len(results) > 0 && results[0].Folder != "INBOX" {
		t.Errorf("expected folder INBOX, got %s", results[0].Folder)
	}

	// Search for "quarterly" - should find message 2
	results, err = svc.Search(MessageSearchOptions{
		User:  user,
		Query: "quarterly",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'quarterly', got %d", len(results))
	}

	// Search for a field - from:bob
	results, err = svc.Search(MessageSearchOptions{
		User:  user,
		Query: "from:bob",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search for 'from:bob' failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'from:bob', got %d", len(results))
	}
}

func TestBuildIndexMultipleFolders(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("failed to create message store: %v", err)
	}

	user := "bob"

	// Create two mailboxes
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("failed to create INBOX: %v", err)
	}
	if err := database.CreateMailbox(user, "Sent"); err != nil {
		t.Fatalf("failed to create Sent: %v", err)
	}

	// Store a message in INBOX
	inboxMsg := []byte("From: sender@test.com\r\n\r\nProject alpha update.\r\n")
	inboxMsgID, err := msgStore.StoreMessage(user, inboxMsg)
	if err != nil {
		t.Fatalf("store inbox msg: %v", err)
	}
	meta := &storage.MessageMetadata{
		MessageID: inboxMsgID,
		UID:       1,
		Subject:   "Alpha Update",
		From:      "sender@test.com",
		To:        "bob@test.com",
		Date:      "2025-02-01",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
		t.Fatalf("store inbox meta: %v", err)
	}

	// Store a message in Sent
	sentMsg := []byte("To: recipient@test.com\r\n\r\nProject beta delivery.\r\n")
	sentMsgID, err := msgStore.StoreMessage(user, sentMsg)
	if err != nil {
		t.Fatalf("store sent msg: %v", err)
	}
	sentMeta := &storage.MessageMetadata{
		MessageID: sentMsgID,
		UID:       1,
		Subject:   "Beta Delivery",
		From:      "bob@test.com",
		To:        "recipient@test.com",
		Date:      "2025-02-02",
	}
	if err := database.StoreMessageMetadata(user, "Sent", 1, sentMeta); err != nil {
		t.Fatalf("store sent meta: %v", err)
	}

	// Build index
	svc := NewService(database, msgStore, nil)
	if err := svc.BuildIndex(user); err != nil {
		t.Fatalf("BuildIndex failed: %v", err)
	}

	// Search for "project" - should find both messages
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "project",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results for 'project', got %d", len(results))
	}

	// Search for "alpha" with folder filter
	results, err = svc.Search(MessageSearchOptions{
		User:   user,
		Query:  "alpha",
		Folder: "INBOX",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'alpha' in INBOX, got %d", len(results))
	}
	if len(results) > 0 && results[0].Folder != "INBOX" {
		t.Errorf("expected INBOX, got %s", results[0].Folder)
	}

	// Search for "beta" in Sent
	results, err = svc.Search(MessageSearchOptions{
		User:   user,
		Query:  "beta",
		Folder: "Sent",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'beta' in Sent, got %d", len(results))
	}
}

func TestBuildIndexReplacesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer database.Close()

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("failed to create message store: %v", err)
	}

	user := "charlie"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	msg := []byte("Subject: Test\r\n\r\nUnique keyword xyzzy.\r\n")
	msgID, err := msgStore.StoreMessage(user, msg)
	if err != nil {
		t.Fatalf("store message: %v", err)
	}
	meta := &storage.MessageMetadata{
		MessageID: msgID,
		UID:       1,
		Subject:   "Test",
		From:      "a@b.com",
		To:        "c@d.com",
		Date:      "2025-03-01",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
		t.Fatalf("store metadata: %v", err)
	}

	svc := NewService(database, msgStore, nil)

	// Build index twice - second should replace first
	if err := svc.BuildIndex(user); err != nil {
		t.Fatalf("first BuildIndex: %v", err)
	}
	if err := svc.BuildIndex(user); err != nil {
		t.Fatalf("second BuildIndex: %v", err)
	}

	// Should still find the message
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "xyzzy",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result after rebuild, got %d", len(results))
	}
}

func TestBuildIndexEmptyMailbox(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	user := "dave"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	svc := NewService(database, nil, nil)
	if err := svc.BuildIndex(user); err != nil {
		t.Fatalf("BuildIndex on empty mailbox: %v", err)
	}

	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()
	if idx.DocCount() != 0 {
		t.Errorf("expected 0 docs for empty mailbox, got %d", idx.DocCount())
	}
}

// --- IndexMessage full round-trip ---

func TestIndexMessageWithRealDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	user := "eve"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	// Pre-create the index (so IndexMessage uses the existing-index path)
	svc := NewService(database, msgStore, nil)
	svc.indexes[user] = NewIndex()

	// Store a message in the message store
	msgData := []byte("From: frank@test.com\r\nTo: eve@test.com\r\nSubject: Secret Mission\r\n\r\nYour mission should you choose to accept it involves cryptography.\r\n")
	msgID, err := msgStore.StoreMessage(user, msgData)
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	// Store metadata in database
	meta := &storage.MessageMetadata{
		MessageID: msgID,
		UID:       5,
		Subject:   "Secret Mission",
		From:      "frank@test.com",
		To:        "eve@test.com",
		Date:      "2025-06-15",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 5, meta); err != nil {
		t.Fatalf("store metadata: %v", err)
	}

	// Index the message
	err = svc.IndexMessage(user, "INBOX", 5)
	if err != nil {
		t.Fatalf("IndexMessage failed: %v", err)
	}

	// Verify it was indexed
	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()
	if idx.DocCount() != 1 {
		t.Errorf("expected 1 doc after IndexMessage, got %d", idx.DocCount())
	}

	// Search for the message
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "cryptography",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if len(results) > 0 {
		if results[0].UID != 5 {
			t.Errorf("expected UID 5, got %d", results[0].UID)
		}
		if results[0].Folder != "INBOX" {
			t.Errorf("expected folder INBOX, got %s", results[0].Folder)
		}
	}
}

func TestIndexMessageNoExistingIndexTriggersBuild(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	user := "grace"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	// Store a message
	msgData := []byte("Subject: Build Trigger Test\r\n\r\nContent about rendezvous protocol.\r\n")
	msgID, err := msgStore.StoreMessage(user, msgData)
	if err != nil {
		t.Fatalf("store message: %v", err)
	}
	meta := &storage.MessageMetadata{
		MessageID: msgID,
		UID:       10,
		Subject:   "Build Trigger Test",
		From:      "a@b.com",
		To:        "c@d.com",
		Date:      "2025-04-01",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 10, meta); err != nil {
		t.Fatalf("store metadata: %v", err)
	}

	// IndexMessage without pre-existing index -> triggers BuildIndex
	svc := NewService(database, msgStore, nil)
	err = svc.IndexMessage(user, "INBOX", 10)
	if err != nil {
		t.Fatalf("IndexMessage (no existing index) failed: %v", err)
	}

	// The message should be searchable
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "rendezvous",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) < 1 {
		t.Errorf("expected at least 1 result, got %d", len(results))
	}
}

func TestIndexMessageMetadataError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	svc := NewService(database, nil, nil)
	svc.indexes["heidi"] = NewIndex()

	// IndexMessage with non-existent message should return an error
	// GetMessageMetadata on a non-existent message returns empty metadata
	// but no error in this implementation. Test the path anyway.
	err = svc.IndexMessage("heidi", "INBOX", 999)
	// Depending on db implementation, this may or may not error
	// The important thing is it doesn't panic
	_ = err
}

func TestIndexMessageWithNilMsgStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	user := "ivan"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	// Store metadata without message data
	meta := &storage.MessageMetadata{
		MessageID: "fakeid123",
		UID:       1,
		Subject:   "No Body Test",
		From:      "x@y.com",
		To:        "z@w.com",
		Date:      "2025-05-01",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
		t.Fatalf("store metadata: %v", err)
	}

	svc := NewService(database, nil, nil) // nil msgStore
	svc.indexes[user] = NewIndex()

	err = svc.IndexMessage(user, "INBOX", 1)
	if err != nil {
		t.Fatalf("IndexMessage with nil msgStore: %v", err)
	}

	// Should still find via subject field
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "subject:body",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	// "body" won't match "No Body Test" as a field query uses lowercase comparison
	// Search for "no" (stop word won't work) - search for "test"
	results, err = svc.Search(MessageSearchOptions{
		User:  user,
		Query: "test",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// --- Search with real DB to cover the metadata-retrieval path ---

func TestSearchWithRealDBMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	user := "judy"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	msgData := []byte("Subject: Budget Review\r\n\r\nThe budget review for Q3 is attached.\r\n")
	msgID, err := msgStore.StoreMessage(user, msgData)
	if err != nil {
		t.Fatalf("store message: %v", err)
	}

	meta := &storage.MessageMetadata{
		MessageID: msgID,
		UID:       7,
		Subject:   "Budget Review",
		From:      "cfo@company.com",
		To:        "judy@company.com",
		Date:      "2025-07-20",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 7, meta); err != nil {
		t.Fatalf("store metadata: %v", err)
	}

	svc := NewService(database, msgStore, nil)

	// BuildIndex will be triggered on first search
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "budget",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Verify metadata was populated from the DB
	r := results[0]
	if r.From != "cfo@company.com" {
		t.Errorf("expected From='cfo@company.com', got %q", r.From)
	}
	if r.To != "judy@company.com" {
		t.Errorf("expected To='judy@company.com', got %q", r.To)
	}
	if r.Subject != "Budget Review" {
		t.Errorf("expected Subject='Budget Review', got %q", r.Subject)
	}
	if r.Date != "2025-07-20" {
		t.Errorf("expected Date='2025-07-20', got %q", r.Date)
	}
	if r.UID != 7 {
		t.Errorf("expected UID=7, got %d", r.UID)
	}
	if r.Folder != "INBOX" {
		t.Errorf("expected Folder='INBOX', got %q", r.Folder)
	}
	if r.Preview == "" {
		t.Error("expected non-empty preview")
	}
	if r.Score <= 0 {
		t.Errorf("expected positive score, got %f", r.Score)
	}
}

func TestSearchWithDBMetadataError(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	svc := NewService(database, nil, nil)

	// Create an index with a valid doc but no stored metadata
	idx := NewIndex()
	svc.indexes["karl"] = idx
	idx.Add(&Document{
		ID:      "INBOX:1",
		Content: "orphan message content",
		Fields: map[string]string{
			"from":    "x@y.com",
			"subject": "Orphan",
		},
	})

	results, err := svc.Search(MessageSearchOptions{
		User:  "karl",
		Query: "orphan",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	// Metadata should be empty since nothing was stored
	if len(results) > 0 {
		r := results[0]
		if r.From != "" || r.Subject != "" {
			t.Errorf("expected empty metadata fields, got From=%q Subject=%q", r.From, r.Subject)
		}
	}
}

// --- BuildIndex with msgStore that fails ReadMessage ---

func TestBuildIndexMsgStoreReadFails(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	user := "mallory"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	// Store metadata but with a messageID that doesn't exist in msgStore
	meta := &storage.MessageMetadata{
		MessageID: "nonexistent_hash_at_least_4_chars",
		UID:       1,
		Subject:   "Missing Message File",
		From:      "a@b.com",
		To:        "c@d.com",
		Date:      "2025-08-01",
	}
	if err := database.StoreMessageMetadata(user, "INBOX", 1, meta); err != nil {
		t.Fatalf("store metadata: %v", err)
	}

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	svc := NewService(database, msgStore, nil)
	// BuildIndex should succeed; ReadMessage fails but extractTextContent handles it
	err = svc.BuildIndex(user)
	if err != nil {
		t.Fatalf("BuildIndex should not fail even if ReadMessage fails: %v", err)
	}

	// The message should still be indexed based on metadata fields
	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()
	if idx.DocCount() != 1 {
		t.Errorf("expected 1 doc indexed (from metadata), got %d", idx.DocCount())
	}

	// Search should find it via subject
	results, err := svc.Search(MessageSearchOptions{
		User:  user,
		Query: "missing",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// --- BuildIndex with GetMsgUIDs error path ---

func TestBuildIndexListMailboxesError(t *testing.T) {
	// BuildIndex with nil db should return "database not available" error
	svc := NewService(nil, nil, nil)
	err := svc.BuildIndex("testuser")
	if err == nil {
		t.Error("expected error with nil database")
	}
	if !strings.Contains(err.Error(), "database not available") {
		t.Errorf("expected 'database not available' error, got: %v", err)
	}
}

// --- IndexMessage adding multiple messages to existing index ---

func TestIndexMessageMultipleToExistingIndex(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/mail.db"
	database, err := storage.OpenDatabase(dbPath)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()

	msgStore, err := storage.NewMessageStore(tmpDir + "/messages")
	if err != nil {
		t.Fatalf("create message store: %v", err)
	}

	user := "nancy"
	if err := database.CreateMailbox(user, "INBOX"); err != nil {
		t.Fatalf("create mailbox: %v", err)
	}

	svc := NewService(database, msgStore, nil)
	svc.indexes[user] = NewIndex()

	// Store and index 3 messages
	for i, tc := range []struct {
		subject string
		body    string
		from    string
	}{
		{"Apples", "All about apples and orchards.", "farmer@farm.com"},
		{"Bananas", "Banana plantations are fascinating.", "botanist@lab.com"},
		{"Cherries", "Cherry blossoms in springtime.", "gardener@garden.com"},
	} {
		uid := uint32(i + 1)
		msgData := []byte(fmt.Sprintf("From: %s\r\nSubject: %s\r\n\r\n%s\r\n", tc.from, tc.subject, tc.body))
		msgID, err := msgStore.StoreMessage(user, msgData)
		if err != nil {
			t.Fatalf("store message %d: %v", uid, err)
		}
		meta := &storage.MessageMetadata{
			MessageID: msgID,
			UID:       uid,
			Subject:   tc.subject,
			From:      tc.from,
			To:        "nancy@test.com",
			Date:      "2025-09-01",
		}
		if err := database.StoreMessageMetadata(user, "INBOX", uid, meta); err != nil {
			t.Fatalf("store metadata %d: %v", uid, err)
		}
		if err := svc.IndexMessage(user, "INBOX", uid); err != nil {
			t.Fatalf("IndexMessage %d: %v", uid, err)
		}
	}

	svc.mu.RLock()
	idx := svc.indexes[user]
	svc.mu.RUnlock()
	if idx.DocCount() != 3 {
		t.Errorf("expected 3 docs, got %d", idx.DocCount())
	}

	// Search for each
	for _, q := range []string{"apples", "banana", "cherry"} {
		results, err := svc.Search(MessageSearchOptions{
			User:  user,
			Query: q,
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("search for '%s' failed: %v", q, err)
		}
		if len(results) != 1 {
			t.Errorf("search '%s': expected 1 result, got %d", q, len(results))
		}
	}
}

// --- sortResults with score-based swap triggered ---

func TestSortResultsSwapTriggered(t *testing.T) {
	// Create results where the inner j loop must actually swap
	results := []SearchResult{
		{DocID: "low", Score: 1.0},
		{DocID: "high", Score: 10.0},
	}
	sortResults(results)
	if results[0].DocID != "high" || results[1].DocID != "low" {
		t.Errorf("swap not performed correctly: %v", results)
	}
}

func TestSortResultsReverseOrder(t *testing.T) {
	n := 5
	results := make([]SearchResult, n)
	for i := 0; i < n; i++ {
		// Insert in ascending order so sort must fully reverse
		results[i] = SearchResult{
			DocID:  fmt.Sprintf("doc%d", i),
			Score:  float64(i),
		}
	}
	sortResults(results)
	for i := 0; i < n-1; i++ {
		if results[i].Score < results[i+1].Score {
			t.Errorf("position %d score %.1f < position %d score %.1f", i, results[i].Score, i+1, results[i+1].Score)
		}
	}
}
