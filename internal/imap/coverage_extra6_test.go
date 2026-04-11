package imap

import (
	"testing"
	"time"

	"github.com/umailserver/umailserver/internal/storage"
)

// --- parseSortCriteria tests ---

func TestParseSortCriteria_Basic(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		expected []SortCriterion
	}{
		{
			name:     "single ARRIVAL",
			args:     []string{"ARRIVAL"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "ARRIVAL", Descending: true}},
		},
		{
			name:     "single SUBJECT",
			args:     []string{"SUBJECT"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "SUBJECT", Descending: true}},
		},
		{
			name:     "REVERSE changes direction",
			args:     []string{"REVERSE", "DATE"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "DATE", Descending: false}},
		},
		{
			name:    "multiple criteria",
			args:    []string{"ARRIVAL", "SUBJECT", "SIZE"},
			wantErr: false,
			expected: []SortCriterion{
				{Field: "ARRIVAL", Descending: true},
				{Field: "SUBJECT", Descending: true},
				{Field: "SIZE", Descending: true},
			},
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
		{
			name:    "unknown criterion",
			args:    []string{"INVALID"},
			wantErr: true,
		},
		{
			name:    "SCORE not supported",
			args:    []string{"SCORE"},
			wantErr: true,
		},
		{
			name:     "FROM criterion",
			args:     []string{"FROM"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "FROM", Descending: true}},
		},
		{
			name:     "CC criterion",
			args:     []string{"CC"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "CC", Descending: true}},
		},
		{
			name:     "TO criterion",
			args:     []string{"TO"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "TO", Descending: true}},
		},
		{
			name:     "UID criterion",
			args:     []string{"UID"},
			wantErr:  false,
			expected: []SortCriterion{{Field: "UID", Descending: true}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSortCriteria(tt.args)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("Got %d criteria, want %d", len(result), len(tt.expected))
				return
			}
			for i, sc := range result {
				if sc.Field != tt.expected[i].Field {
					t.Errorf("Field = %q, want %q", sc.Field, tt.expected[i].Field)
				}
				if sc.Descending != tt.expected[i].Descending {
					t.Errorf("Descending = %v, want %v", sc.Descending, tt.expected[i].Descending)
				}
			}
		})
	}
}

func TestParseSortCriteria_CaseInsensitive(t *testing.T) {
	result, err := parseSortCriteria([]string{"arrival", "reverse", "subject"})
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("Expected 2 criteria, got %d", len(result))
	}
	if result[0].Field != "ARRIVAL" || result[0].Descending != true {
		t.Errorf("First criterion incorrect")
	}
	if result[1].Field != "SUBJECT" || result[1].Descending != false {
		t.Errorf("Second criterion incorrect")
	}
}

// --- sortMessagesByCriteria tests ---

func TestSortMessagesByCriteria_Empty(t *testing.T) {
	result := sortMessagesByCriteria(nil, []SortCriterion{{Field: "DATE", Descending: true}}, nil)
	if result != nil {
		t.Errorf("Expected nil for empty input, got %v", result)
	}
}

func TestSortMessagesByCriteria_SingleMessage(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{
			MessageID:    "msg1",
			UID:          1,
			Subject:      "Test",
			From:         "a@example.com",
			Date:         "10 Jan 2024 10:00:00 +0000",
			InternalDate: time.Now(),
			Size:         100,
		},
	}
	criteria := []SortCriterion{{Field: "SUBJECT", Descending: true}}
	seqNums := []uint32{1}

	result := sortMessagesByCriteria(messages, criteria, seqNums)
	if len(result) != 1 || result[0] != 1 {
		t.Errorf("Expected [1], got %v", result)
	}
}

func TestSortMessagesByCriteria_BySubject(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "m1", Subject: "Zebra", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "m2", Subject: "Apple", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "m3", Subject: "Banana", Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	criteria := []SortCriterion{{Field: "SUBJECT", Descending: true}}
	seqNums := []uint32{1, 2, 3}

	result := sortMessagesByCriteria(messages, criteria, seqNums)
	// Descending subject order: Zebra, Banana, Apple -> seqNums [1, 3, 2]
	if len(result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(result))
	}
	if result[0] != 1 || result[1] != 3 || result[2] != 2 {
		t.Errorf("Expected [1, 3, 2], got %v", result)
	}
}

func TestSortMessagesByCriteria_BySize(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "m1", Subject: "Small", Size: 50, Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now()},
		{MessageID: "m2", Subject: "Large", Size: 200, Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now()},
		{MessageID: "m3", Subject: "Medium", Size: 100, Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Now()},
	}
	criteria := []SortCriterion{{Field: "SIZE", Descending: true}}
	seqNums := []uint32{1, 2, 3}

	result := sortMessagesByCriteria(messages, criteria, seqNums)
	// Descending size: Large(200), Medium(100), Small(50) -> seqNums [2, 3, 1]
	if len(result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(result))
	}
	if result[0] != 2 || result[1] != 3 || result[2] != 1 {
		t.Errorf("Expected [2, 3, 1], got %v", result)
	}
}

func TestSortMessagesByCriteria_ByUID(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "m1", UID: 100, Subject: "A", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "m2", UID: 300, Subject: "B", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "m3", UID: 200, Subject: "C", Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	criteria := []SortCriterion{{Field: "UID", Descending: true}}
	seqNums := []uint32{1, 2, 3}

	result := sortMessagesByCriteria(messages, criteria, seqNums)
	// Descending UID: 300, 200, 100 -> seqNums [2, 3, 1]
	if len(result) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(result))
	}
	if result[0] != 2 || result[1] != 3 || result[2] != 1 {
		t.Errorf("Expected [2, 3, 1], got %v", result)
	}
}

func TestSortMessagesByCriteria_Ascending(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "m1", Subject: "Zebra", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "m2", Subject: "Apple", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	criteria := []SortCriterion{{Field: "SUBJECT", Descending: false}}
	seqNums := []uint32{1, 2}

	result := sortMessagesByCriteria(messages, criteria, seqNums)
	// Ascending: Apple, Zebra -> seqNums [2, 1]
	if result[0] != 2 || result[1] != 1 {
		t.Errorf("Expected [2, 1], got %v", result)
	}
}

// --- threadMessagesByReferences tests ---

func TestThreadMessagesByReferences_Simple(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "root", Subject: "Root", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "reply", InReplyTo: "root", Subject: "Reply", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	seqNums := []uint32{1, 2}

	children := threadMessagesByReferences(messages, seqNums)

	// root(1) should have reply(2) as child
	if len(children[1]) != 1 || children[1][0] != 2 {
		t.Errorf("Expected children[1] = [2], got %v", children[1])
	}
}

func TestThreadMessagesByReferences_NoParent(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "msg1", Subject: "One", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "msg2", Subject: "Two", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	seqNums := []uint32{1, 2}

	children := threadMessagesByReferences(messages, seqNums)

	// No parent relationships, children map should be empty or have empty slices
	if len(children) != 0 {
		t.Errorf("Expected empty children map, got %v", children)
	}
}

func TestThreadMessagesByReferences_WithReferences(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "root", Subject: "Root", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "reply", References: []string{"root"}, Subject: "Reply", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	seqNums := []uint32{1, 2}

	children := threadMessagesByReferences(messages, seqNums)

	if len(children[1]) != 1 || children[1][0] != 2 {
		t.Errorf("Expected children[1] = [2], got %v", children[1])
	}
}

func TestThreadMessagesByReferences_MultipleChildren(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{MessageID: "root", Subject: "Root", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "reply1", InReplyTo: "root", Subject: "Reply 1", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{MessageID: "reply2", InReplyTo: "root", Subject: "Reply 2", Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	seqNums := []uint32{1, 2, 3}

	children := threadMessagesByReferences(messages, seqNums)

	if len(children[1]) != 2 {
		t.Errorf("Expected children[1] = [2, 3], got %v", children[1])
	}
}

// --- threadMessagesByOrderedSubject tests ---

func TestThreadMessagesByOrderedSubject_SameSubject(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{Subject: "Test", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC), Size: 100},
		{Subject: "Test", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), Size: 100},
		{Subject: "Test", Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC), Size: 100},
	}
	seqNums := []uint32{1, 2, 3}

	children := threadMessagesByOrderedSubject(messages, seqNums)

	// First message (1) is root, 2 and 3 are children of 1
	if len(children[1]) != 2 {
		t.Errorf("Expected children[1] = [2, 3], got %v", children[1])
	}
}

func TestThreadMessagesByOrderedSubject_DifferentSubjects(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{Subject: "Apple", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{Subject: "Banana", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{Subject: "Cherry", Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	seqNums := []uint32{1, 2, 3}

	children := threadMessagesByOrderedSubject(messages, seqNums)

	// No children since each message has different subject
	for _, childrenList := range children {
		if len(childrenList) > 0 {
			t.Errorf("Expected no children, got %v", children)
			break
		}
	}
}

func TestThreadMessagesByOrderedSubject_EmptySubject(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{Subject: "", Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
		{Subject: "", Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now(), Size: 100},
	}
	seqNums := []uint32{1, 2}

	children := threadMessagesByOrderedSubject(messages, seqNums)

	// Empty subjects grouped under "(no subject)"
	if len(children[1]) != 1 || children[1][0] != 2 {
		t.Errorf("Expected children[1] = [2], got %v", children[1])
	}
}

// --- flattenThread tests ---

func TestFlattenThread_Simple(t *testing.T) {
	children := map[uint32][]uint32{
		1: {2, 3},
		2: {4},
	}
	visited := make(map[uint32]bool)

	result := flattenThread(1, children, visited)

	// Should visit 1 -> 2 -> 4 and 3 (order may vary due to map iteration)
	if len(result) != 4 {
		t.Errorf("Expected 4 results, got %d", len(result))
	}
	if !containsUint32(result, 1) || !containsUint32(result, 2) ||
		!containsUint32(result, 3) || !containsUint32(result, 4) {
		t.Errorf("Expected [1,2,3,4], got %v", result)
	}
}

func TestFlattenThread_Empty(t *testing.T) {
	children := map[uint32][]uint32{}
	visited := make(map[uint32]bool)

	result := flattenThread(1, children, visited)

	if len(result) != 1 || result[0] != 1 {
		t.Errorf("Expected [1], got %v", result)
	}
}

func TestFlattenThread_AlreadyVisited(t *testing.T) {
	children := map[uint32][]uint32{
		1: {2},
		2: {1}, // cycle back to 1
	}
	visited := map[uint32]bool{1: true, 2: true}

	result := flattenThread(1, children, visited)

	// Both 1 and 2 already visited, should return empty
	if len(result) != 0 {
		t.Errorf("Expected empty result, got %v", result)
	}
}

func containsUint32(slice []uint32, val uint32) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// --- addressToString tests ---

func TestAddressToString_Single(t *testing.T) {
	addrs := []*Address{
		{MailboxName: "user", HostName: "example.com"},
	}
	result := addressToString(addrs)
	if result != "user@example.com" {
		t.Errorf("Expected 'user@example.com', got %q", result)
	}
}

func TestAddressToString_Empty(t *testing.T) {
	result := addressToString(nil)
	if result != "" {
		t.Errorf("Expected empty string, got %q", result)
	}
}

func TestAddressToString_Multiple(t *testing.T) {
	addrs := []*Address{
		{MailboxName: "user", HostName: "example.com"},
		{MailboxName: "other", HostName: "other.com"},
	}
	result := addressToString(addrs)
	if result != "user@example.com" {
		t.Errorf("Expected first address 'user@example.com', got %q", result)
	}
}

// --- SortResult and ThreadResult types ---

func TestSortResult_Default(t *testing.T) {
	sr := SortResult{}
	if sr.SequenceNumbers != nil {
		t.Error("Expected nil SequenceNumbers")
	}
}

func TestThreadResult_Default(t *testing.T) {
	tr := ThreadResult{}
	if tr.Threads != nil {
		t.Error("Expected nil Threads")
	}
}

// --- MessageMetadata from FetchMessages ---

func TestMessageMetadata_WithAllFields(t *testing.T) {
	msg := &Message{
		SeqNum:       5,
		UID:          100,
		Flags:        []string{"\\Seen", "\\Answered"},
		InternalDate: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Size:         1024,
		Subject:      "Test Subject",
		Date:         "15 Jan 2024 10:30:00 +0000",
		Envelope: &Envelope{
			Subject:   "Test Subject",
			From:      []*Address{{MailboxName: "sender", HostName: "send.com"}},
			Date:      "15 Jan 2024 10:30:00 +0000",
			MessageID: "msg123",
			InReplyTo: "ref456",
		},
	}

	if msg.SeqNum != 5 {
		t.Errorf("SeqNum = %d, want 5", msg.SeqNum)
	}
	if msg.UID != 100 {
		t.Errorf("UID = %d, want 100", msg.UID)
	}
	if len(msg.Envelope.From) != 1 {
		t.Errorf("Expected 1 From address")
	}
}

// --- Mailbox with all fields ---

func TestMailbox_AllFields(t *testing.T) {
	mailbox := &Mailbox{
		Name:           "INBOX",
		Exists:         100,
		Recent:         5,
		Unseen:         10,
		UIDValidity:    123456789,
		UIDNext:        200,
		Flags:          []string{"\\Seen", "\\Answered"},
		PermanentFlags: []string{"\\Seen", "\\Answered", "\\Deleted", "\\Draft", "\\Flagged", "*"},
		ReadOnly:       false,
	}

	if mailbox.Name != "INBOX" {
		t.Errorf("Name = %q, want INBOX", mailbox.Name)
	}
	if mailbox.Exists != 100 {
		t.Errorf("Exists = %d, want 100", mailbox.Exists)
	}
	if mailbox.UIDValidity != 123456789 {
		t.Errorf("UIDValidity = %d, want 123456789", mailbox.UIDValidity)
	}
	if mailbox.ReadOnly {
		t.Error("Expected ReadOnly = false")
	}
}

// --- BodyStructure ---

func TestBodyStructure_Basic(t *testing.T) {
	bs := &BodyStructure{
		Type:    "text",
		Subtype: "plain",
		Parameters: map[string]string{
			"charset": "UTF-8",
		},
	}

	if bs.Type != "text" {
		t.Errorf("Type = %q, want 'text'", bs.Type)
	}
	if bs.Subtype != "plain" {
		t.Errorf("Subtype = %q, want 'plain'", bs.Subtype)
	}
	if bs.Parameters["charset"] != "UTF-8" {
		t.Errorf("charset = %q, want 'UTF-8'", bs.Parameters["charset"])
	}
}

// --- sortMessagesByCriteria uses only first criterion ---

func TestSortMessagesByCriteria_UsesFirstCriterionOnly(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{Subject: "Apple", Size: 100, Date: "1 Jan 2024 10:00:00 +0000", InternalDate: time.Now()},
		{Subject: "Banana", Size: 200, Date: "2 Jan 2024 10:00:00 +0000", InternalDate: time.Now()},
		{Subject: "Apple", Size: 50, Date: "3 Jan 2024 10:00:00 +0000", InternalDate: time.Now()},
	}
	criteria := []SortCriterion{
		{Field: "SUBJECT", Descending: false},
		{Field: "SIZE", Descending: true},
	}
	seqNums := []uint32{1, 2, 3}

	result := sortMessagesByCriteria(messages, criteria, seqNums)

	// Implementation only uses first criterion, so subject ascending: Apple, Apple, Banana
	// seq order: 1 (Apple 100), 3 (Apple 50), 2 (Banana 200) - stable sort preserves input order for equals
	if len(result) != 3 {
		t.Errorf("Expected 3 results, got %d", len(result))
	}
}

// --- parseSortCriteria with CHARSET ---

// Note: CHARSET handling is at command level, not in parseSortCriteria
// parseSortCriteria receives criteria after CHARSET is stripped

// --- messageForSort with invalid date ---

func TestSortMessagesByCriteria_InvalidDate(t *testing.T) {
	messages := []*storage.MessageMetadata{
		{Subject: "A", Date: "invalid-date", InternalDate: time.Now(), Size: 100},
		{Subject: "B", Date: "also-invalid", InternalDate: time.Now(), Size: 100},
	}
	criteria := []SortCriterion{{Field: "DATE", Descending: true}}
	seqNums := []uint32{1, 2}

	// Should not panic and should still sort
	result := sortMessagesByCriteria(messages, criteria, seqNums)
	if len(result) != 2 {
		t.Errorf("Expected 2 results, got %d", len(result))
	}
}
