package carddav

import (
	"encoding/json"
	"errors"
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
	// migrated records usernames whose legacy directory was already examined.
	migrated sync.Map
}

// NewStorage creates a new CardDAV storage
func NewStorage(dataDir string) *Storage {
	return &Storage{
		dataDir: filepath.Join(dataDir, "carddav"),
	}
}

// legacyUserKey is the pre-round-214 directory name of a user: "@" became
// "_at_". It is not injective ("a@b" and "a_at_b" shared one namespace).
func legacyUserKey(username string) string {
	return strings.ReplaceAll(username, "@", "_at_")
}

// userKey maps a username to its directory name injectively (F6910), as
// CalDAV does (F6160). Names whose legacy mapping is unambiguous keep it, so
// existing directories stay in place; any other name is percent-encoded
// byte-wise, which always contains "%" and so never equals a legacy name.
func userKey(username string) string {
	legacy := legacyUserKey(username)
	if username != "." && !strings.Contains(username, "%") && strings.ReplaceAll(legacy, "_at_", "@") == username {
		return legacy
	}
	return escapeUserKey(username)
}

func escapeUserKey(username string) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(username); i++ {
		c := username[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&15])
		}
	}
	return b.String()
}

// userDir returns the directory for a user's address books. A legacy
// directory is renamed into place on first access only when its owner is
// unambiguous (its legacy name round-trips to the username).
func (s *Storage) userDir(username string) string {
	key := userKey(username)
	dir := filepath.Join(s.dataDir, key)
	if legacy := legacyUserKey(username); legacy != key && strings.ReplaceAll(legacy, "_at_", "@") == username && username != "." {
		if _, loaded := s.migrated.LoadOrStore(username, true); !loaded {
			old := filepath.Join(s.dataDir, legacy)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				if _, err := os.Stat(old); err == nil {
					_ = os.Rename(old, dir)
				}
			}
		}
	}
	return dir
}

// errInvalidID marks an identifier that would resolve outside its parent
// directory; handlers map it to a client error.
var errInvalidID = errors.New("invalid identifier")

// validateID ensures an identifier names exactly one entry inside its parent
// directory: no separators, no ".." sequence, and not the "." self-reference.
// Every storage entry point that builds a path from a request-derived ID calls
// it (F5085: GET/COPY/MOVE read other users' contacts via "../"; F5086: MKCOL
// created address books in other users' namespaces).
func validateID(id string) error {
	if id == "." || strings.Contains(id, "..") || strings.Contains(id, string(filepath.Separator)) || strings.Contains(id, "/") {
		return fmt.Errorf("%w: %s", errInvalidID, id)
	}
	return nil
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
	if err := validateID(ab.ID); err != nil {
		return err
	}
	now := time.Now()
	ab.Created = now
	ab.Modified = now

	dir := s.addressbookDir(username, ab.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create addressbook directory: %w", err)
	}

	data, err := json.MarshalIndent(ab, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal addressbook: %w", err)
	}

	path := s.addressbookPath(username, ab.ID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write addressbook: %w", err)
	}

	return nil
}

// GetAddressbook retrieves an address book by ID
func (s *Storage) GetAddressbook(username, addressbookID string) (*Addressbook, error) {
	if err := validateID(addressbookID); err != nil {
		return nil, err
	}
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
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("failed to read addressbook %s: %w", entry.Name(), err)
			}
			if ab != nil {
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
	if err := validateID(ab.ID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ab.Modified = time.Now()

	data, err := json.MarshalIndent(ab, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal addressbook: %w", err)
	}

	path := s.addressbookPath(username, ab.ID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write addressbook: %w", err)
	}

	return nil
}

// DeleteAddressbook deletes an address book and all its contacts
func (s *Storage) DeleteAddressbook(username, addressbookID string) error {
	if addressbookID == "" {
		return fmt.Errorf("%w: empty", errInvalidID)
	}
	if err := validateID(addressbookID); err != nil {
		return err
	}
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
	if err := validateID(addressbookID); err != nil {
		return err
	}
	if err := validateID(contact.UID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure addressbook directory exists
	dir := s.addressbookDir(username, addressbookID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create addressbook directory: %w", err)
	}

	// Write the raw vCard data
	path := s.contactPath(username, addressbookID, contact.UID)
	// #nosec G703 -- IDs are validated with validateID before use
	if err := os.WriteFile(path, []byte(vcardData), 0o600); err != nil {
		return fmt.Errorf("failed to write contact: %w", err)
	}

	// Update addressbook modification time
	if ab, err := s.getAddressbookUnsafe(username, addressbookID); err == nil && ab != nil {
		ab.Modified = time.Now()
		data, _ := json.MarshalIndent(ab, "", "  ")
		// #nosec G703 -- IDs are validated with validateID before use
		_ = os.WriteFile(filepath.Clean(s.addressbookPath(username, addressbookID)), data, 0o600)
	}

	return nil
}

// GetContact retrieves a contact
func (s *Storage) GetContact(username, addressbookID, contactUID string) (string, error) {
	if err := validateID(addressbookID); err != nil {
		return "", err
	}
	if err := validateID(contactUID); err != nil {
		return "", err
	}
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
	if err := validateID(addressbookID); err != nil {
		return nil, err
	}
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
			if err != nil {
				return nil, fmt.Errorf("failed to read contact %s: %w", entry.Name(), err)
			}
			contacts = append(contacts, string(data))
		}
	}

	return contacts, nil
}

// DeleteContact deletes a contact
func (s *Storage) DeleteContact(username, addressbookID, contactUID string) error {
	if err := validateID(addressbookID); err != nil {
		return err
	}
	if err := validateID(contactUID); err != nil {
		return err
	}

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
		// #nosec G703 -- IDs are validated with validateID before use
		_ = os.WriteFile(filepath.Clean(s.addressbookPath(username, addressbookID)), data, 0o600)
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
	return fmt.Sprintf("\"%d\"", info.ModTime().UnixNano())
}

// GetAddressbookETag generates an ETag for an addressbook
func (s *Storage) GetAddressbookETag(username, addressbookID string) string {
	path := s.addressbookPath(username, addressbookID)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("\"%s\"", uuid.New().String())
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().UnixNano())
}

// PurgeUser removes every address book and contact of a user, including
// directories that only differ in letter case. A user with no data is not an
// error, so the call is safe to retry (F6810).
func (s *Storage) PurgeUser(username string) error {
	if username == "" || username == "." || strings.ContainsAny(username, "/\\\x00") || strings.Contains(username, "..") {
		return fmt.Errorf("%w: %q", errInvalidID, username)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	names := []string{userKey(username)}
	if legacy := legacyUserKey(username); legacy != names[0] && strings.ReplaceAll(legacy, "_at_", "@") == username {
		names = append(names, legacy)
	}
	entries, err := os.ReadDir(s.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("purge carddav user: %w", err)
	}
	var errs []error
	for _, e := range entries {
		for _, n := range names {
			if strings.EqualFold(e.Name(), n) {
				if err := os.RemoveAll(filepath.Join(s.dataDir, e.Name())); err != nil {
					errs = append(errs, err)
				}
				break
			}
		}
	}
	s.migrated.Delete(username)
	return errors.Join(errs...)
}

// PurgeUser removes all CardDAV data of the user (F6810).
func (s *Server) PurgeUser(username string) error { return s.storage.PurgeUser(username) }
