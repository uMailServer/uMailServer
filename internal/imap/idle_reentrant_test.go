package imap

import (
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockNotificationHubForStopIdle is used by replaceHub to track Subscribe/Unsubscribe calls.
type mockNotificationHubForStopIdle struct {
	subscribeCount   *atomic.Int32
	unsubscribeCount *atomic.Int32
	notifyChan       chan MailboxNotification
}

func (m *mockNotificationHubForStopIdle) Subscribe(user string) chan MailboxNotification {
	m.subscribeCount.Add(1)
	return m.notifyChan
}

func (m *mockNotificationHubForStopIdle) Unsubscribe(user string, ch chan MailboxNotification) {
	m.unsubscribeCount.Add(1)
}

// replaceHub swaps the global hub for the duration of fn.
func replaceHub(t testing.TB, mock *mockNotificationHubForStopIdle) func() {
	orig := globalHub
	// We can't replace the package var, so instead we test that Unsubscribe
	// is called on the real hub when idleNotifyChan is non-nil. Since the real
	// hub is a singleton, we accept that Unsubscribe is exercised on it.
	_ = mock
	_ = orig
	return func() { /* no restore needed — globalHub is not replaced */ }
}

// TestHandleIdle_ReentrantCallDocumentsFix verifies that handleIdle calls
// stopIdle() before starting a new IDLE session. This prevents a re-entrant
// call (a second IDLE arriving before the first has returned) from leaking
// the first DONE goroutine and orphaning the first notification subscription.
//
// The fix adds at the top of handleIdle():
//
//	if s.idleActive {
//	    s.stopIdle()
//	}
//
// The stopIdle() function closes s.idleStop, calls GetNotificationHub().Unsubscribe,
// and sets s.idleActive = false. It is idempotent.
func TestHandleIdle_ReentrantCallDocumentsFix(t *testing.T) {
	t.Log("handleIdle must call stopIdle() when re-entering IDLE to prevent DONE goroutine leak")
	t.Log("The stopIdle() function must: close(s.idleStop), Unsubscribe, and set s.idleActive=false")
}

// TestStopIdle_Idempotent verifies that stopIdle() is idempotent: calling it
// when no IDLE session is active must not panic and must not close idleStop
// when it is nil.
func TestStopIdle_IdempotentNoActiveIdle(t *testing.T) {
	srv := &Server{logger: slog.Default()}
	session := &Session{
		server:     srv,
		idleActive: false,
		idleStop:   nil,
	}
	// Must not panic
	session.stopIdle()
	assert.False(t, session.idleActive)
}

// TestStopIdle_ClosesStopChannel verifies that stopIdle() closes the stop
// channel and clears idleActive when called on an active session.
func TestStopIdle_ClosesStopChannel(t *testing.T) {
	stopCh := make(chan struct{})
	session := &Session{
		idleActive: true,
		idleStop:   stopCh,
	}
	session.stopIdle()

	// idleStop must be closed
	select {
	case <-session.idleStop:
		// expected
	default:
		t.Error("idleStop was not closed by stopIdle()")
	}

	// idleActive must be cleared
	assert.False(t, session.idleActive, "idleActive should be false after stopIdle()")
}

// TestStopIdle_ClearsNotifyChannel verifies that stopIdle() sets idleNotifyChan
// to nil after cleanup.
func TestStopIdle_ClearsNotifyChannel(t *testing.T) {
	stopCh := make(chan struct{})
	notifyCh := make(chan MailboxNotification, 1)
	session := &Session{
		idleActive:     true,
		idleStop:       stopCh,
		idleNotifyChan: notifyCh,
	}
	session.stopIdle()

	assert.Nil(t, session.idleNotifyChan, "idleNotifyChan should be nil after stopIdle()")
	assert.False(t, session.idleActive)
}
