package carddav

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewStorage(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	if storage == nil {
		t.Fatal("NewStorage returned nil")
	}

	if storage.dataDir == "" {
		t.Error("dataDir should not be empty")
	}
}

func TestCreateAddressbook(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	tests := []struct {
		name     string
		username string
		ab       *Addressbook
		wantErr  bool
	}{
		{
			name:     "create new addressbook with ID",
			username: "user1@example.com",
			ab: &Addressbook{
				ID:          "personal",
				Name:        "Personal Addressbook",
				Description: "My personal contacts",
			},
			wantErr: false,
		},
		{
			name:     "create addressbook without ID generates UUID",
			username: "user1@example.com",
			ab: &Addressbook{
				Name: "No ID Addressbook",
			},
			wantErr: false,
		},
		{
			name:     "create addressbook with special chars in username",
			username: "user+test@example.com",
			ab: &Addressbook{
				ID:   "work",
				Name: "Work Addressbook",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := storage.CreateAddressbook(tt.username, tt.ab)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateAddressbook() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.ab.ID == "" {
				t.Error("Addressbook ID should have been generated")
			}

			if tt.ab.Created.IsZero() {
				t.Error("Addressbook Created time should be set")
			}

			if tt.ab.Modified.IsZero() {
				t.Error("Addressbook Modified time should be set")
			}
		})
	}
}

func TestGetAddressbook(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create an addressbook first
	ab := &Addressbook{
		ID:          "test-ab",
		Name:        "Test Addressbook",
		Description: "Test Description",
	}
	err := storage.CreateAddressbook("user1@example.com", ab)
	if err != nil {
		t.Fatalf("Failed to create addressbook: %v", err)
	}

	tests := []struct {
		name        string
		username    string
		addressbookID string
		wantNil     bool
		wantErr     bool
	}{
		{
			name:        "get existing addressbook",
			username:    "user1@example.com",
			addressbookID: "test-ab",
			wantNil:     false,
			wantErr:     false,
		},
		{
			name:        "get non-existent addressbook",
			username:    "user1@example.com",
			addressbookID: "nonexistent",
			wantNil:     true,
			wantErr:     false,
		},
		{
			name:        "get from non-existent user",
			username:    "user2@example.com",
			addressbookID: "test-ab",
			wantNil:     true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storage.GetAddressbook(tt.username, tt.addressbookID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetAddressbook() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantNil && got != nil {
				t.Errorf("GetAddressbook() = %v, want nil", got)
			}
			if !tt.wantNil && got == nil {
				t.Error("GetAddressbook() = nil, want non-nil")
			}
		})
	}
}

func TestGetAddressbooks(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Initially should return empty slice
	abs, err := storage.GetAddressbooks("user1@example.com")
	if err != nil {
		t.Errorf("GetAddressbooks() error = %v", err)
	}
	if len(abs) != 0 {
		t.Errorf("GetAddressbooks() = %d, want 0", len(abs))
	}

	// Create some addressbooks
	addressbooks := []*Addressbook{
		{ID: "ab1", Name: "Addressbook 1"},
		{ID: "ab2", Name: "Addressbook 2"},
		{ID: "ab3", Name: "Addressbook 3"},
	}

	for _, ab := range addressbooks {
		if err := storage.CreateAddressbook("user1@example.com", ab); err != nil {
			t.Fatalf("Failed to create addressbook: %v", err)
		}
	}

	// Now should return all addressbooks
	abs, err = storage.GetAddressbooks("user1@example.com")
	if err != nil {
		t.Errorf("GetAddressbooks() error = %v", err)
	}
	if len(abs) != 3 {
		t.Errorf("GetAddressbooks() = %d, want 3", len(abs))
	}
}

func TestUpdateAddressbook(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create an addressbook
	ab := &Addressbook{
		ID:          "test-ab",
		Name:        "Original Name",
		Description: "Original Description",
	}
	if err := storage.CreateAddressbook("user1@example.com", ab); err != nil {
		t.Fatalf("Failed to create addressbook: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Update the addressbook
	ab.Name = "Updated Name"
	ab.Description = "Updated Description"
	if err := storage.UpdateAddressbook("user1@example.com", ab); err != nil {
		t.Errorf("UpdateAddressbook() error = %v", err)
	}

	// Verify update
	updated, err := storage.GetAddressbook("user1@example.com", "test-ab")
	if err != nil {
		t.Fatalf("Failed to get addressbook: %v", err)
	}

	if updated.Name != "Updated Name" {
		t.Errorf("Name = %s, want Updated Name", updated.Name)
	}

	if updated.Description != "Updated Description" {
		t.Errorf("Description = %s, want Updated Description", updated.Description)
	}

	if !updated.Modified.After(updated.Created) {
		t.Error("Modified time should be after Created time")
	}
}

func TestDeleteAddressbook(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create an addressbook
	ab := &Addressbook{ID: "to-delete", Name: "To Delete"}
	if err := storage.CreateAddressbook("user1@example.com", ab); err != nil {
		t.Fatalf("Failed to create addressbook: %v", err)
	}

	// Delete it
	if err := storage.DeleteAddressbook("user1@example.com", "to-delete"); err != nil {
		t.Errorf("DeleteAddressbook() error = %v", err)
	}

	// Verify it's gone
	_, err := storage.GetAddressbook("user1@example.com", "to-delete")
	if err != nil {
		t.Errorf("GetAddressbook() error = %v", err)
	}
}

func TestSaveContact(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create an addressbook first
	ab := &Addressbook{ID: "test-ab", Name: "Test Addressbook"}
	if err := storage.CreateAddressbook("user1@example.com", ab); err != nil {
		t.Fatalf("Failed to create addressbook: %v", err)
	}

	vcardData := `BEGIN:VCARD
VERSION:3.0
UID:test-contact-123
FN:John Doe
EMAIL:john@example.com
TEL:+1234567890
END:VCARD`

	contact := &Contact{
		UID:      "test-contact-123",
		FullName: "John Doe",
	}

	err := storage.SaveContact("user1@example.com", "test-ab", contact, vcardData)
	if err != nil {
		t.Errorf("SaveContact() error = %v", err)
	}

	// Verify contact was saved
	savedData, err := storage.GetContact("user1@example.com", "test-ab", "test-contact-123")
	if err != nil {
		t.Errorf("GetContact() error = %v", err)
	}
	if savedData == "" {
		t.Error("Contact should have been saved")
	}
}

func TestGetContact(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test Addressbook"}
	storage.CreateAddressbook("user1@example.com", ab)

	vcardData := "BEGIN:VCARD\nUID:test-contact\nEND:VCARD"
	contact := &Contact{UID: "test-contact"}
	storage.SaveContact("user1@example.com", "test-ab", contact, vcardData)

	tests := []struct {
		name         string
		username     string
		addressbookID string
		contactUID   string
		wantData     bool
		wantErr      bool
	}{
		{
			name:         "get existing contact",
			username:     "user1@example.com",
			addressbookID: "test-ab",
			contactUID:   "test-contact",
			wantData:     true,
			wantErr:      false,
		},
		{
			name:         "get non-existent contact",
			username:     "user1@example.com",
			addressbookID: "test-ab",
			contactUID:   "nonexistent",
			wantData:     false,
			wantErr:      false,
		},
		{
			name:         "get from non-existent addressbook",
			username:     "user1@example.com",
			addressbookID: "nonexistent",
			contactUID:   "test-contact",
			wantData:     false,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := storage.GetContact(tt.username, tt.addressbookID, tt.contactUID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetContact() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantData && got == "" {
				t.Error("GetContact() returned empty data, want non-empty")
			}
			if !tt.wantData && got != "" {
				t.Errorf("GetContact() = %v, want empty", got)
			}
		})
	}
}

func TestGetContacts(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test Addressbook"}
	storage.CreateAddressbook("user1@example.com", ab)

	// Initially should return empty
	contacts, err := storage.GetContacts("user1@example.com", "test-ab")
	if err != nil {
		t.Errorf("GetContacts() error = %v", err)
	}
	if len(contacts) != 0 {
		t.Errorf("GetContacts() = %d, want 0", len(contacts))
	}

	// Add some contacts
	for i := 0; i < 3; i++ {
		vcardData := `BEGIN:VCARD
UID:contact-` + string(rune('a'+i)) + `
FN:Contact ` + string(rune('A'+i)) + `
END:VCARD`
		contact := &Contact{UID: "contact-" + string(rune('a'+i))}
		storage.SaveContact("user1@example.com", "test-ab", contact, vcardData)
	}

	// Now should return all contacts
	contacts, err = storage.GetContacts("user1@example.com", "test-ab")
	if err != nil {
		t.Errorf("GetContacts() error = %v", err)
	}
	if len(contacts) != 3 {
		t.Errorf("GetContacts() = %d, want 3", len(contacts))
	}
}

func TestDeleteContact(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test Addressbook"}
	storage.CreateAddressbook("user1@example.com", ab)

	vcardData := "BEGIN:VCARD\nUID:test-contact\nEND:VCARD"
	contact := &Contact{UID: "test-contact"}
	storage.SaveContact("user1@example.com", "test-ab", contact, vcardData)

	// Delete the contact
	if err := storage.DeleteContact("user1@example.com", "test-ab", "test-contact"); err != nil {
		t.Errorf("DeleteContact() error = %v", err)
	}

	// Delete non-existent should not error
	if err := storage.DeleteContact("user1@example.com", "test-ab", "nonexistent"); err != nil {
		t.Errorf("DeleteContact() non-existent error = %v", err)
	}
}

func TestGetETag(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook and contact
	ab := &Addressbook{ID: "test-ab", Name: "Test Addressbook"}
	storage.CreateAddressbook("user1@example.com", ab)

	// Get ETag for non-existent contact (should generate UUID)
	etag := storage.GetETag("user1@example.com", "test-ab", "nonexistent")
	if etag == "" {
		t.Error("GetETag() should return non-empty for non-existent")
	}

	// Create contact
	vcardData := "BEGIN:VCARD\nUID:test-contact\nEND:VCARD"
	contact := &Contact{UID: "test-contact"}
	storage.SaveContact("user1@example.com", "test-ab", contact, vcardData)

	// Get ETag for existing contact
	etag = storage.GetETag("user1@example.com", "test-ab", "test-contact")
	if etag == "" {
		t.Error("GetETag() should return non-empty")
	}

	if !isValidCardDAVETag(etag) {
		t.Errorf("GetETag() = %s, not a valid ETag", etag)
	}
}

func TestGetAddressbookETag(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Get ETag for non-existent addressbook
	etag := storage.GetAddressbookETag("user1@example.com", "nonexistent")
	if etag == "" {
		t.Error("GetAddressbookETag() should return non-empty for non-existent")
	}

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test Addressbook"}
	storage.CreateAddressbook("user1@example.com", ab)

	// Get ETag for existing addressbook
	etag = storage.GetAddressbookETag("user1@example.com", "test-ab")
	if etag == "" {
		t.Error("GetAddressbookETag() should return non-empty")
	}

	if !isValidCardDAVETag(etag) {
		t.Errorf("GetAddressbookETag() = %s, not a valid ETag", etag)
	}
}

func TestUserDirSanitization(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	tests := []struct {
		username string
		want     string
	}{
		{
			username: "user@example.com",
			want:     "user_at_example.com",
		},
		{
			username: "plainuser",
			want:     "plainuser",
		},
		{
			username: "user+tag@example.com",
			want:     "user+tag_at_example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.username, func(t *testing.T) {
			got := storage.userDir(tt.username)
			wantPath := filepath.Join(storage.dataDir, tt.want)
			if got != wantPath {
				t.Errorf("userDir() = %v, want %v", got, wantPath)
			}
		})
	}
}

func TestAddressbookPathFunctions(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	username := "user@example.com"
	addressbookID := "my-addressbook"
	contactUID := "contact-123"

	// Test addressbookDir
	abDir := storage.addressbookDir(username, addressbookID)
	wantAbDir := filepath.Join(storage.dataDir, "user_at_example.com", addressbookID)
	if abDir != wantAbDir {
		t.Errorf("addressbookDir() = %v, want %v", abDir, wantAbDir)
	}

	// Test contactPath
	contactPath := storage.contactPath(username, addressbookID, contactUID)
	wantContactPath := filepath.Join(abDir, contactUID+".vcf")
	if contactPath != wantContactPath {
		t.Errorf("contactPath() = %v, want %v", contactPath, wantContactPath)
	}

	// Test addressbookPath
	abPath := storage.addressbookPath(username, addressbookID)
	wantAbPath := filepath.Join(abDir, ".addressbook.json")
	if abPath != wantAbPath {
		t.Errorf("addressbookPath() = %v, want %v", abPath, wantAbPath)
	}
}

func isValidCardDAVETag(etag string) bool {
	return len(etag) > 2 && etag[0] == '"' && etag[len(etag)-1] == '"'
}

func TestConcurrentCardDAVAccess(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook
	ab := &Addressbook{ID: "concurrent-ab", Name: "Concurrent Test"}
	if err := storage.CreateAddressbook("user@example.com", ab); err != nil {
		t.Fatalf("Failed to create addressbook: %v", err)
	}

	// Concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			vcardData := `BEGIN:VCARD
UID:contact-` + string(rune('a'+idx)) + `
FN:Contact ` + string(rune('A'+idx)) + `
END:VCARD`
			contact := &Contact{UID: "contact-" + string(rune('a'+idx))}
			storage.SaveContact("user@example.com", "concurrent-ab", contact, vcardData)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify all contacts were saved
	contacts, err := storage.GetContacts("user@example.com", "concurrent-ab")
	if err != nil {
		t.Errorf("GetContacts() error = %v", err)
	}
	if len(contacts) != 10 {
		t.Errorf("Got %d contacts, want 10", len(contacts))
	}
}

func TestContactStruct(t *testing.T) {
	now := time.Now()
	contact := Contact{
		UID:          "test-uid",
		FullName:     "John Doe",
		FirstName:    "John",
		LastName:     "Doe",
		Organization: "Example Corp",
		Title:        "Engineer",
		Note:         "Test note",
		Created:      now,
		Modified:     now,
		Email: []Email{
			{Type: "work", Value: "john@example.com", Primary: true},
			{Type: "home", Value: "john.home@example.com"},
		},
		Phone: []Phone{
			{Type: "work", Value: "+1234567890"},
			{Type: "mobile", Value: "+0987654321"},
		},
		Address: []Address{
			{Type: "work", Street: "123 Work St", City: "Work City", Region: "CA", PostalCode: "12345", Country: "USA"},
		},
	}

	if contact.UID != "test-uid" {
		t.Error("UID mismatch")
	}
	if contact.FullName != "John Doe" {
		t.Error("FullName mismatch")
	}
	if len(contact.Email) != 2 {
		t.Errorf("Email count = %d, want 2", len(contact.Email))
	}
	if len(contact.Phone) != 2 {
		t.Errorf("Phone count = %d, want 2", len(contact.Phone))
	}
	if len(contact.Address) != 1 {
		t.Errorf("Address count = %d, want 1", len(contact.Address))
	}
}

func TestAddressbookStruct(t *testing.T) {
	now := time.Now()
	ab := Addressbook{
		ID:          "test-id",
		Name:        "Test Addressbook",
		Description: "Test Description",
		ReadOnly:    true,
		Created:     now,
		Modified:    now,
	}

	if ab.ID != "test-id" {
		t.Error("ID mismatch")
	}
	if ab.Name != "Test Addressbook" {
		t.Error("Name mismatch")
	}
	if ab.Description != "Test Description" {
		t.Error("Description mismatch")
	}
	if !ab.ReadOnly {
		t.Error("ReadOnly should be true")
	}
}

func TestStorageWithRealFilesystem(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create multiple users with multiple addressbooks
	users := []string{"alice@example.com", "bob@example.com"}
	for _, user := range users {
		for i := 0; i < 3; i++ {
			ab := &Addressbook{
				ID:   fmt.Sprintf("ab-%d", i),
				Name: fmt.Sprintf("Addressbook %d", i),
			}
			if err := storage.CreateAddressbook(user, ab); err != nil {
				t.Errorf("Failed to create addressbook for %s: %v", user, err)
			}
		}
	}

	// Verify filesystem structure
	for _, user := range users {
		userPath := storage.userDir(user)
		if _, err := os.Stat(userPath); os.IsNotExist(err) {
			t.Errorf("User directory should exist: %s", userPath)
		}
	}
}

// Test GetAddressbook returns nil for non-existent
func TestGetAddressbook_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	ab, err := storage.GetAddressbook("user", "nonexistent")
	// Function returns nil, nil for non-existent addressbook
	if err != nil {
		t.Errorf("Expected no error for non-existent addressbook, got: %v", err)
	}
	if ab != nil {
		t.Error("Expected nil addressbook for non-existent")
	}
}

// Test GetContact returns nil for non-existent
func TestGetContact_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook first
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	storage.CreateAddressbook("user", ab)

	contact, err := storage.GetContact("user", "test-ab", "nonexistent")
	// Function returns empty string, nil for non-existent contact
	if err != nil {
		t.Errorf("Expected no error for non-existent contact, got: %v", err)
	}
	if contact != "" {
		t.Errorf("Expected empty contact for non-existent, got: %s", contact)
	}
}

// Test GetContacts returns empty slice for non-existent addressbook
func TestGetContacts_NonExistentAddressbook(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	contacts, err := storage.GetContacts("user", "nonexistent")
	// Function returns empty slice, nil for non-existent addressbook
	if err != nil {
		t.Errorf("Expected no error for non-existent addressbook, got: %v", err)
	}
	if len(contacts) != 0 {
		t.Errorf("Expected empty slice for non-existent addressbook, got %d contacts", len(contacts))
	}
}

// Test GetETag with non-existent contact
func TestGetETag_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	// Create addressbook
	ab := &Addressbook{ID: "test-ab", Name: "Test"}
	storage.CreateAddressbook("user", ab)

	etag := storage.GetETag("user", "test-ab", "nonexistent")
	// Function generates a new UUID ETag for non-existent contact
	if etag == "" {
		t.Error("Expected non-empty ETag for non-existent contact")
	}
}

// Test GetAddressbookETag with non-existent addressbook
func TestGetAddressbookETag_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	storage := NewStorage(tmpDir)

	etag := storage.GetAddressbookETag("user", "nonexistent")
	// Function generates a new UUID ETag for non-existent addressbook
	if etag == "" {
		t.Error("Expected non-empty ETag for non-existent addressbook")
	}
}
