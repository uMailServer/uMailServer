package imap

import (
	"strings"
	"testing"
	"time"
)

// ---------- handleStatus with various status items ----------

func TestCoverageHandleStatus_AllItems(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STATUS INBOX (MESSAGES RECENT UIDNEXT UIDVALIDITY UNSEEN)")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "MESSAGES") {
		t.Errorf("Expected MESSAGES in status response, got: %s", resp)
	}
	<-done
}

func TestCoverageHandleStatus_NoArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STATUS")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for STATUS without args, got: %s", resp)
	}
	<-done
}

// ---------- parseFetchItems extra coverage ----------

func TestCoverageParseFetchItems_Parenthesized(t *testing.T) {
	items := parseFetchItems([]string{"(FLAGS", "UID", "RFC822.SIZE)"})
	if len(items) != 3 {
		t.Errorf("Expected 3 items, got %d: %v", len(items), items)
	}
}

func TestCoverageParseFetchItems_SingleItem(t *testing.T) {
	items := parseFetchItems([]string{"FLAGS"})
	if len(items) != 1 || strings.ToUpper(items[0]) != "FLAGS" {
		t.Errorf("Expected [FLAGS], got %v", items)
	}
}

// ---------- formatFetchResponse coverage ----------

func TestCoverageFormatFetchResponse_AllItems(t *testing.T) {
	now := time.Now()
	msg := &Message{
		SeqNum:       1,
		UID:          42,
		Flags:        []string{"\\Seen", "\\Flagged"},
		InternalDate: now,
		Size:         1024,
		Subject:      "Test Subject",
		From:         "sender@example.com",
		To:           "recipient@example.com",
		Date:         "Mon, 1 Jan 2024 00:00:00 +0000",
		Envelope: &Envelope{
			Date:      "Mon, 1 Jan 2024 00:00:00 +0000",
			Subject:   "Test Subject",
			From:      []*Address{{PersonalName: "", MailboxName: "sender", HostName: "example.com"}},
			To:        []*Address{{PersonalName: "", MailboxName: "recipient", HostName: "example.com"}},
			MessageID: "<test@example.com>",
		},
		BodyStructure: &BodyStructure{
			Type:       "text",
			Subtype:    "plain",
			Parameters: map[string]string{"charset": "utf-8"},
			Size:       1024,
			Lines:      10,
		},
		Data: []byte("Subject: Test\r\n\r\nBody"),
	}

	items := []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "UID", "ENVELOPE", "BODYSTRUCTURE", "RFC822"}
	resp := formatFetchResponse(msg, items)
	if !strings.Contains(resp, "FLAGS") {
		t.Error("Expected FLAGS in response")
	}
	if !strings.Contains(resp, "UID 42") {
		t.Error("Expected UID 42 in response")
	}
	if !strings.Contains(resp, "ENVELOPE") {
		t.Error("Expected ENVELOPE in response")
	}
	if !strings.Contains(resp, "BODYSTRUCTURE") {
		t.Error("Expected BODYSTRUCTURE in response")
	}
	if !strings.Contains(resp, "RFC822") {
		t.Error("Expected RFC822 in response")
	}
}

// ---------- handleLogin success/failure ----------

func TestCoverageHandleLogin2_Success(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN test password")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "OK") {
		t.Errorf("Expected OK for LOGIN, got: %s", resp)
	}
	<-done
}

func TestCoverageHandleLogin2_Failure(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN wrong wrongpass")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NO") {
		t.Errorf("Expected NO for bad LOGIN, got: %s", resp)
	}
	<-done
}

func TestCoverageHandleLogin2_MissingArgs(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGIN")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BAD") {
		t.Errorf("Expected BAD for LOGIN without args, got: %s", resp)
	}
	<-done
}

// ---------- handleAuthPlain via handleCommand ----------

func TestCoverageHandleAuthPlain2_ViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		// base64("test\0test\0password") = dGVzdAB0ZXN0AHBhc3N3b3Jk
		done <- session.handleCommand("A001 AUTHENTICATE PLAIN dGVzdAB0ZXN0AHBhc3N3b3Jk")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleAuthLogin via handleCommand ----------

func TestCoverageHandleAuthLogin2_ViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateNotAuthenticated, "", nil)
	defer client.Close()

	authDone := make(chan error, 1)
	go func() {
		authDone <- session.handleCommand("A001 AUTHENTICATE LOGIN")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("dGVzdA==\r\n")) // base64("test")
	}
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("cGFzc3dvcmQ=\r\n")) // base64("password")
	}

	_ = drainConn(client, 200*time.Millisecond)
	<-authDone
}

// ---------- handleLogout ----------

func TestCoverageHandleLogout2_ViaCommand(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LOGOUT")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "BYE") {
		t.Errorf("Expected BYE in LOGOUT response, got: %s", resp)
	}
	<-done
}

// ---------- handleCapability in authenticated state ----------

func TestCoverageHandleCapability2_Authenticated(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CAPABILITY")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "CAPABILITY") {
		t.Errorf("Expected CAPABILITY response, got: %s", resp)
	}
	<-done
}

// ---------- handleNoop authenticated ----------

func TestCoverageHandleNoop2_Authenticated(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 NOOP")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "OK") {
		t.Errorf("Expected OK for NOOP, got: %s", resp)
	}
	<-done
}

// ---------- handleAuthenticated NAMESPACE/ENABLE ----------

func TestCoverageHandleNamespace2_Authenticated(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 NAMESPACE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "NAMESPACE") {
		t.Errorf("Expected NAMESPACE response, got: %s", resp)
	}
	<-done
}

func TestCoverageHandleEnable2(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 ENABLE CONDSTORE")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "ENABLED") {
		t.Errorf("Expected ENABLED response, got: %s", resp)
	}
	<-done
}

// ---------- handleAuthenticated SUBSCRIBE/UNSUBSCRIBE ----------

func TestCoverageHandleSubscribe2_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SUBSCRIBE INBOX")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleUnsubscribe2_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 UNSUBSCRIBE INBOX")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleDelete/Create/Rename with nil mailstore ----------

func TestCoverageHandleDelete2_NilMailstoreAuth(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 DELETE SomeFolder")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleCreate2_NilMailstoreAuth(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 CREATE TestFolder")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleRename2_NilMailstoreAuth(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 RENAME OldName NewName")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleSelect/Examine with nil mailstore ----------

func TestCoverageHandleSelect2_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SELECT INBOX")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleExamine2_NilMailstore(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 EXAMINE INBOX")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleAppend with flags ----------

func TestCoverageHandleAppend2_WithFlags(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX (\\Seen \\Flagged) {13}")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("Hello, World!\r\n"))
	}

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleAppend with date ----------

func TestCoverageHandleAppend2_WithDate(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 APPEND INBOX (\\Seen) \"01-Jan-2024 00:00:00 +0000\" {5}")
	}()

	lines := scanLines(client)
	if _, ok := waitForLine(lines, "+", 500*time.Millisecond); ok {
		client.Write([]byte("Hello\r\n"))
	}

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleList with patterns ----------

func TestCoverageHandleList2_StarPattern(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LIST \"\" *")
	}()

	resp := drainConn(client, 200*time.Millisecond)
	if !strings.Contains(resp, "LIST") {
		t.Errorf("Expected LIST response, got: %s", resp)
	}
	<-done
}

func TestCoverageHandleList2_PercentPattern(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LIST \"\" %")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleLsub coverage ----------

func TestCoverageHandleLsub2_Pattern(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateAuthenticated, "test", nil)
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 LSUB \"\" *")
	}()

	// handleLsub delegates to handleList, so response contains LIST not LSUB
	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleSearch various criteria ----------

func TestCoverageHandleSearch2_FlagCriteria(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH SEEN UNSEEN FLAGGED UNFLAGGED DELETED UNDELETED ANSWERED UNANSWERED")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleSearch2_DateCriteria(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH SINCE 01-Jan-2024 BEFORE 01-Feb-2024 ON 15-Jan-2024")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleSearch2_SizeCriteria(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH LARGER 100 SMALLER 10000")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleSearch2_FromSubjectTo(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH FROM alice SUBJECT test TO bob")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleSearch2_NotOr(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 SEARCH NOT SEEN OR SEEN FLAGGED")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleFetch with parenthesized items ----------

func TestCoverageHandleFetch2_ParenthesizedItems(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 FETCH 1 (FLAGS UID RFC822.SIZE)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleUIDFetch coverage ----------

func TestCoverageHandleUIDFetch2_Flags(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 UID FETCH 1:* (FLAGS)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

// ---------- handleStore coverage ----------

func TestCoverageHandleStore2_RemoveFlags(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 -FLAGS (\\Seen)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}

func TestCoverageHandleStore2_FlagsSilent(t *testing.T) {
	client, session := setupSessionWithPipe(t, StateSelected, "test", &Mailbox{Name: "INBOX"})
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- session.handleCommand("A001 STORE 1 +FLAGS.SILENT (\\Seen)")
	}()

	_ = drainConn(client, 200*time.Millisecond)
	<-done
}
