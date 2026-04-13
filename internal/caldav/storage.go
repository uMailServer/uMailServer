package caldav

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

// Storage handles persistence for calendars and events
type Storage struct {
	dataDir string
	mu      sync.RWMutex
}

// NewStorage creates a new CalDAV storage
func NewStorage(dataDir string) *Storage {
	return &Storage{
		dataDir: filepath.Join(dataDir, "caldav"),
	}
}

// userDir returns the directory for a user's calendars
func (s *Storage) userDir(username string) string {
	// Sanitize username for filesystem
	safeUsername := strings.ReplaceAll(username, "@", "_at_")
	return filepath.Join(s.dataDir, safeUsername)
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
	now := time.Now()
	cal.Created = now
	cal.Modified = now

	dir := s.calendarDir(username, cal.ID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create calendar directory: %w", err)
	}

	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal calendar: %w", err)
	}

	path := s.calendarPath(username, cal.ID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write calendar: %w", err)
	}

	return nil
}

// GetCalendar retrieves a calendar by ID
func (s *Storage) GetCalendar(username, calendarID string) (*Calendar, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.calendarPath(username, calendarID)
	data, err := os.ReadFile(path)
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
			if err == nil && cal != nil {
				calendars = append(calendars, cal)
			}
		}
	}

	return calendars, nil
}

// getCalendarUnsafe reads a calendar without locking (caller must hold lock)
func (s *Storage) getCalendarUnsafe(username, calendarID string) (*Calendar, error) {
	path := s.calendarPath(username, calendarID)
	data, err := os.ReadFile(path)
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
	s.mu.Lock()
	defer s.mu.Unlock()

	cal.Modified = time.Now()

	data, err := json.MarshalIndent(cal, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal calendar: %w", err)
	}

	path := s.calendarPath(username, cal.ID)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write calendar: %w", err)
	}

	return nil
}

// DeleteCalendar deletes a calendar and all its events
func (s *Storage) DeleteCalendar(username, calendarID string) error {
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
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure calendar directory exists
	dir := s.calendarDir(username, calendarID)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create calendar directory: %w", err)
	}

	// Write the raw iCalendar data
	path := s.eventPath(username, calendarID, event.UID)
	if err := os.WriteFile(path, []byte(icsData), 0600); err != nil {
		return fmt.Errorf("failed to write event: %w", err)
	}

	// Update calendar modification time
	if cal, err := s.getCalendarUnsafe(username, calendarID); err == nil && cal != nil {
		cal.Modified = time.Now()
		data, _ := json.MarshalIndent(cal, "", "  ")
		_ = os.WriteFile(s.calendarPath(username, calendarID), data, 0600)
	}

	return nil
}

// GetEvent retrieves a calendar event
func (s *Storage) GetEvent(username, calendarID, eventUID string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	path := s.eventPath(username, calendarID, eventUID)
	data, err := os.ReadFile(path)
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
			data, err := os.ReadFile(path)
			if err == nil {
				events = append(events, string(data))
			}
		}
	}

	return events, nil
}

// DeleteEvent deletes a calendar event
func (s *Storage) DeleteEvent(username, calendarID, eventUID string) error {
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
		_ = os.WriteFile(s.calendarPath(username, calendarID), data, 0600)
	}

	return nil
}

// GetETag generates an ETag for an event based on modification time
func (s *Storage) GetETag(username, calendarID, eventUID string) string {
	path := s.eventPath(username, calendarID, eventUID)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("\"%s\"", uuid.New().String())
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().Unix())
}

// GetCalendarETag generates an ETag for a calendar
func (s *Storage) GetCalendarETag(username, calendarID string) string {
	path := s.calendarPath(username, calendarID)
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Sprintf("\"%s\"", uuid.New().String())
	}
	return fmt.Sprintf("\"%d\"", info.ModTime().Unix())
}
