package carddav

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Storage handles persistence for address books and contacts
type Storage struct {
	dataDir string
	mu      sync.RWMutex
}

// NewStorage creates a new CardDAV storage
func NewStorage(dataDir string) *Storage {
	return &Storage{
		dataDir: filepath.Join(dataDir, "carddav"),
	}
}

// userDir returns the directory for a user's address books
func (s *Storage) userDir(username string) string {
	// Sanitize username for filesystem
	safeUsername := strings.ReplaceAll(username, "@", "_at_")
	return filepath.Join(s.dataDir, safeUsername)
}

// addressbookDir returns the directory for a specific address book
func (s *Storage) addressbookDir(username, addressbookID string) string {
	return filepath.Join(s.userDir(username), addressbookID)
}

// contactPath returns the file path for a specific contact
func (s *Storage) contactPath(username, addressbookID, contactUID string) string {
	return filepath.Join(s.addressbookDir(username, addressbookID), contactUID+".vcf")
}

// addressbookPath returns the file path for addressbook metadata
func (s *Storage) addressbookPath(username, addressbookID string) string {
	return filepath.Join(s.addressbookDir(username, addressbookID), ".addressbook.json")
}

// CreateAddressbook creates a new address book for a user
func (s *Storage) CreateAddressbook(username string, ab *Addressbook) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ab.ID == "" {
		ab.ID = uuid.New().String()
	}
	now := time.Now()
	ab.Created = now
	ab.Modified = now

	dir := s.addressbookDir(username, ab.ID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create addressbook directory: %w", err)
	}

	data, err := json.MarshalIndent(ab, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal addressbook: %w", err)
	}

	path := s.addressbookPath(username, ab.ID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write addressbook: %w", err)
	}

	return nil
}

// GetAddressbook retrieves an address book by ID
func (s *Storage) GetAddressbook(username, addressbookID string) (*Addressbook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.addressbookPath(username, addressbookID)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read addressbook: %w", err)
	}

	var ab Addressbook
	if err := json.Unmarshal(data, &ab); err != nil {
		return nil, fmt.Errorf("failed to unmarshal addressbook: %w", err)
	}

	return &ab, nil
}

// GetAddressbooks returns all address books for a user
func (s *Storage) GetAddressbooks(username string) ([]*Addressbook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	userPath := s.userDir(username)
	entries, err := os.ReadDir(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Addressbook{}, nil
		}
		return nil, fmt.Errorf("failed to read user directory: %w", err)
	}

	var addressbooks []*Addressbook
	for _, entry := range entries {
		if entry.IsDir() {
			ab, err := s.getAddressbookUnsafe(username, entry.Name())
			if err == nil && ab != nil {
				addressbooks = append(addressbooks, ab)
			}
		}
	}

	return addressbooks, nil
}

// getAddressbookUnsafe reads an addressbook without locking (caller must hold lock)
func (s *Storage) getAddressbookUnsafe(username, addressbookID string) (*Addressbook, error) {
	path := s.addressbookPath(username, addressbookID)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var ab Addressbook
	if err := json.Unmarshal(data, &ab); err != nil {
		return nil, err
	}

	return &ab, nil
}

// UpdateAddressbook updates an address book
func (s *Storage) UpdateAddressbook(username string, ab *Addressbook) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	ab.Modified = time.Now()

	data, err := json.MarshalIndent(ab, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal addressbook: %w", err)
	}

	path := s.addressbookPath(username, ab.ID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write addressbook: %w", err)
	}

	return nil
}

// DeleteAddressbook deletes an address book and all its contacts
func (s *Storage) DeleteAddressbook(username, addressbookID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.addressbookDir(username, addressbookID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete addressbook: %w", err)
	}

	return nil
}

// SaveContact saves a contact
func (s *Storage) SaveContact(username, addressbookID string, contact *Contact, vcardData string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure addressbook directory exists
	dir := s.addressbookDir(username, addressbookID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create addressbook directory: %w", err)
	}

	// Write the raw vCard data
	path := s.contactPath(username, addressbookID, contact.UID)
	if err := os.WriteFile(path, []byte(vcardData), 0600); err != nil {
		return fmt.Errorf("failed to write contact: %w", err)
	}

	// Update addressbook modification time
	if ab, err := s.getAddressbookUnsafe(username, addressbookID); err == nil && ab != nil {
		ab.Modified = time.Now()
		data, _ := json.MarshalIndent(ab, "", "  ")
		_ = os.WriteFile(s.addressbookPath(username, addressbookID), data, 0600)
	}

	return nil
}

// GetContact retrieves a contact
func (s *Storage) GetContact(username, addressbookID, contactUID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.contactPath(username, addressbookID, contactUID)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read contact: %w", err)
	}

	return string(data), nil
}

// GetContacts returns all contacts in an address book
func (s *Storage) GetContacts(username, addressbookID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.addressbookDir(username, addressbookID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read addressbook directory: %w", err)
	}

	var contacts []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".vcf") {
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(filepath.Clean(path))
			if err == nil {
				contacts = append(contacts, string(data))
			}
		}
	}

	return contacts, nil
}

// DeleteContact deletes a contact
func (s *Storage) DeleteContact(username, addressbookID, contactUID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.contactPath(username, addressbookID, contactUID)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to delete contact: %w", err)
	}

	// Update addressbook modification time
	if ab, err := s.getAddressbookUnsafe(username, addressbookID); err == nil && ab != nil {
		ab.Modified = time.Now()
		data, _ := json.MarshalIndent(ab, "", "  ")
		_ = os.WriteFile(s.addressbookPath(username, addressbookID), data, 0600)
	}

	return nil
}

// GetETag generates an ETag for a contact based on modification time
func (s *Storage) GetETag(username, addressbookID, contactUID string) string {
	path := s.contactPath(username, addressbookID, contactUID)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("\"%s\"", uuid.New().String())
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().Unix())
}

// GetAddressbookETag generates an ETag for an addressbook
func (s *Storage) GetAddressbookETag(username, addressbookID string) string {
	path := s.addressbookPath(username, addressbookID)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("\"%s\"", uuid.New().String())
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().Unix())
}
