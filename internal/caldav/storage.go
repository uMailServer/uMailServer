package caldav

import (
	"crypto/sha256"
	"encoding/hex"
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

// Storage handles persistence for calendars and events
type Storage struct {
	dataDir string
	mu      sync.RWMutex
	// migrated records usernames whose legacy directory was already examined.
	migrated sync.Map
}

// NewStorage creates a new CalDAV storage
func NewStorage(dataDir string) *Storage {
	return &Storage{
		dataDir: filepath.Join(dataDir, "caldav"),
	}
}

// legacyUserKey is the pre-round-134 directory name of a user: "@" became
// "_at_". It is not injective ("a@b" and "a_at_b" collided, sharing one
// calendar namespace).
func legacyUserKey(username string) string {
	return strings.ReplaceAll(username, "@", "_at_")
}

// userKey maps a username to its directory name injectively (F6160). Names
// whose legacy mapping is unambiguous keep it, so existing data directories
// stay in place; any other name (a literal "_at_", a "%", or ".") is
// percent-encoded byte-wise, which always contains a "%" and so can never
// equal a legacy name.
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

// userDir returns the directory for a user's calendars
func (s *Storage) userDir(username string) string {
	key := userKey(username)
	dir := filepath.Join(s.dataDir, key)
	// Migrate a legacy directory only when its owner is unambiguous: a name
	// that contains "%" and whose legacy mapping round-trips. Ambiguous
	// legacy directories ("a_at_b") stay with the "@" interpretation.
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
// directory: no separators, no ".." sequence, and not the "." self-reference
// (F5087: unvalidated IDs let MKCALENDAR/GET escape the user's namespace).
func validateID(id string) error {
	if id == "" || len(id) > 200 || strings.ContainsRune(id, 0) || id == "." || strings.Contains(id, "..") || strings.Contains(id, string(filepath.Separator)) || strings.Contains(id, "/") {
		return fmt.Errorf("%w: %s", errInvalidID, id)
	}
	return nil
}

// calendarDir returns the directory for a specific calendar
func (s *Storage) calendarDir(username, calendarID string) string {
	return filepath.Join(s.userDir(username), calendarID)
}

// eventPath returns the file path for a specific event
func (s *Storage) eventPath(username, calendarID, eventUID string) string {
	return filepath.Join(s.calendarDir(username, calendarID), eventUID+".ics")
}

// calendarPath returns the file path for calendar metadata
func (s *Storage) calendarPath(username, calendarID string) string {
	return filepath.Join(s.calendarDir(username, calendarID), ".calendar.json")
}

// CreateCalendar creates a new calendar for a user
func (s *Storage) CreateCalendar(username string, cal *Calendar) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if cal.ID == "" {
		cal.ID = uuid.New().String()
	}
	if err := validateID(cal.ID); err != nil {
		return err
	}
	now := time.Now()
	cal.Created = now
	cal.Modified = now

	dir := s.calendarDir(username, cal.ID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create calendar directory: %w", err)
	}

	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal calendar: %w", err)
	}

	path := s.calendarPath(username, cal.ID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write calendar: %w", err)
	}

	return nil
}

// GetCalendar retrieves a calendar by ID
func (s *Storage) GetCalendar(username, calendarID string) (*Calendar, error) {
	if err := validateID(calendarID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.calendarPath(username, calendarID)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read calendar: %w", err)
	}

	var cal Calendar
	if err := json.Unmarshal(data, &cal); err != nil {
		return nil, fmt.Errorf("failed to unmarshal calendar: %w", err)
	}

	return &cal, nil
}

// GetCalendars returns all calendars for a user
func (s *Storage) GetCalendars(username string) ([]*Calendar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	userPath := s.userDir(username)
	entries, err := os.ReadDir(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*Calendar{}, nil
		}
		return nil, fmt.Errorf("failed to read user directory: %w", err)
	}

	var calendars []*Calendar
	for _, entry := range entries {
		if entry.IsDir() {
			cal, err := s.getCalendarUnsafe(username, entry.Name())
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return nil, fmt.Errorf("failed to read calendar %s: %w", entry.Name(), err)
			}
			if cal != nil {
				calendars = append(calendars, cal)
			}
		}
	}

	return calendars, nil
}

// getCalendarUnsafe reads a calendar without locking (caller must hold lock)
func (s *Storage) getCalendarUnsafe(username, calendarID string) (*Calendar, error) {
	path := s.calendarPath(username, calendarID)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var cal Calendar
	if err := json.Unmarshal(data, &cal); err != nil {
		return nil, err
	}

	return &cal, nil
}

// UpdateCalendar updates a calendar
func (s *Storage) UpdateCalendar(username string, cal *Calendar) error {
	if err := validateID(cal.ID); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	cal.Modified = time.Now()

	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal calendar: %w", err)
	}

	path := s.calendarPath(username, cal.ID)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write calendar: %w", err)
	}

	return nil
}

// DeleteCalendar deletes a calendar and all its events
func (s *Storage) DeleteCalendar(username, calendarID string) error {
	if calendarID == "" || calendarID == "." {
		return fmt.Errorf("invalid identifier: %s", calendarID)
	}
	if err := validateID(calendarID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	dir := s.calendarDir(username, calendarID)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("failed to delete calendar: %w", err)
	}

	return nil
}

// SaveEvent saves a calendar event
func (s *Storage) SaveEvent(username, calendarID string, event *CalendarEvent, icsData string) error {
	if err := validateID(calendarID); err != nil {
		return err
	}
	if err := validateID(event.UID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure calendar directory exists
	dir := s.calendarDir(username, calendarID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create calendar directory: %w", err)
	}

	// Write the raw iCalendar data
	path := s.eventPath(username, calendarID, event.UID)
	// #nosec G703 -- IDs are validated with validateID before use
	if err := os.WriteFile(path, []byte(icsData), 0o600); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	// Update calendar modification time
	if cal, err := s.getCalendarUnsafe(username, calendarID); err == nil && cal != nil {
		cal.Modified = time.Now()
		data, _ := json.MarshalIndent(cal, "", "  ")
		// #nosec G703 -- IDs are validated with validateID before use
		_ = os.WriteFile(filepath.Clean(s.calendarPath(username, calendarID)), data, 0o600)
	}

	return nil
}

// GetEvent retrieves a calendar event
func (s *Storage) GetEvent(username, calendarID, eventUID string) (string, error) {
	if err := validateID(calendarID); err != nil {
		return "", err
	}
	if err := validateID(eventUID); err != nil {
		return "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.eventPath(username, calendarID, eventUID)
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to read event: %w", err)
	}

	return string(data), nil
}

// GetEvents returns all events in a calendar
func (s *Storage) GetEvents(username, calendarID string) ([]string, error) {
	if err := validateID(calendarID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.calendarDir(username, calendarID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read calendar directory: %w", err)
	}

	var events []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".ics") {
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(filepath.Clean(path))
			if err != nil {
				return nil, fmt.Errorf("failed to read event %s: %w", entry.Name(), err)
			}
			events = append(events, string(data))
		}
	}

	return events, nil
}

// EventEntry is one stored event resource: Name is the resource name used in
// its URL (the file name without ".ics"), which need not equal the UID inside
// the iCalendar data (RFC 4791 §4.1 / §5.3.2: clients choose the name).
type EventEntry struct {
	Name string
	Data string
	ETag string
}

// GetEventEntries returns every event resource with its name and ETag from a
// single directory pass (no per-event stat), sorted by name.
func (s *Storage) GetEventEntries(username, calendarID string) ([]EventEntry, error) {
	if err := validateID(calendarID); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.entriesUnsafe(username, calendarID)
}

func (s *Storage) entriesUnsafe(username, calendarID string) ([]EventEntry, error) {
	dir := s.calendarDir(username, calendarID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read calendar directory: %w", err)
	}
	var out []EventEntry
	for _, entry := range entries {
		n := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(n, ".ics") {
			continue
		}
		data, err := os.ReadFile(filepath.Clean(filepath.Join(dir, n)))
		if err != nil {
			return nil, fmt.Errorf("failed to read event %s: %w", n, err)
		}
		e := EventEntry{Name: strings.TrimSuffix(n, ".ics"), Data: string(data)}
		if info, err := entry.Info(); err == nil {
			e.ETag = fmt.Sprintf("\"%d\"", info.ModTime().UnixNano())
		}
		out = append(out, e)
	}
	return out, nil
}

// FindUIDConflict returns the name of another resource in the calendar whose
// iCalendar UID equals uid (RFC 4791 §5.3.2.1 no-uid-conflict), or "".
// Resources named exceptName are ignored.
func (s *Storage) FindUIDConflict(username, calendarID, uid, exceptName string) (string, error) {
	if err := validateID(calendarID); err != nil {
		return "", err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := s.entriesUnsafe(username, calendarID)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		if e.Name == exceptName || (len(uid) < 60 && !strings.Contains(e.Data, uid)) {
			continue
		}
		if extractUIDFromICS(e.Data) == uid {
			return e.Name, nil
		}
	}
	return "", nil
}

// DeleteEvent deletes a calendar event
func (s *Storage) DeleteEvent(username, calendarID, eventUID string) error {
	if err := validateID(calendarID); err != nil {
		return err
	}
	if err := validateID(eventUID); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	path := s.eventPath(username, calendarID, eventUID)
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to delete event: %w", err)
	}

	// Update calendar modification time
	if cal, err := s.getCalendarUnsafe(username, calendarID); err == nil && cal != nil {
		cal.Modified = time.Now()
		data, _ := json.MarshalIndent(cal, "", "  ")
		// #nosec G703 -- IDs are validated with validateID before use
		_ = os.WriteFile(filepath.Clean(s.calendarPath(username, calendarID)), data, 0o600)
	}

	return nil
}

// GetETag generates an ETag for an event based on modification time. It
// returns "" when the file cannot be inspected: an ETag must be stable, so a
// fresh random value (which could never match an If-Match) is not invented
// (F5655). Callers treat "" as unavailable.
func (s *Storage) GetETag(username, calendarID, eventUID string) string {
	path := s.eventPath(username, calendarID, eventUID)
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().UnixNano())
}

// GetCalendarETag generates an ETag for a calendar, or "" when the metadata
// cannot be inspected (F5655).
func (s *Storage) GetCalendarETag(username, calendarID string) string {
	path := s.calendarPath(username, calendarID)
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().UnixNano())
}

// GetCalendarCTag returns a collection tag that changes whenever the
// calendar or any of its events is written or removed (F5751). It hashes the
// calendar metadata (whose Modified time every event write refreshes) with
// the name, size and mtime of every event, so it is robust even when a
// metadata rewrite is lost or timestamps are coarse. "" means unavailable.
func (s *Storage) GetCalendarCTag(username, calendarID string) string {
	if validateID(calendarID) != nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	meta, err := os.ReadFile(filepath.Clean(s.calendarPath(username, calendarID)))
	if err != nil {
		return ""
	}
	h := sha256.New()
	h.Write(meta)
	entries, err := os.ReadDir(s.calendarDir(username, calendarID))
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".ics") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		fmt.Fprintf(h, "|%s:%d:%d", e.Name(), info.Size(), info.ModTime().UnixNano())
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}
