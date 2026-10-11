package server

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/umailserver/umailserver/internal/caldav"
	"github.com/umailserver/umailserver/internal/carddav"
	"github.com/umailserver/umailserver/internal/imap"
	"github.com/umailserver/umailserver/internal/pop3"
	"github.com/umailserver/umailserver/internal/storage"
)

// pop3MailstoreAdapter adapts imap.BboltMailstore and storage.MessageStore
// to satisfy the pop3.Mailstore interface for POP3 access.
type pop3MailstoreAdapter struct {
	mailstore *imap.BboltMailstore
	msgStore  *storage.MessageStore
}

func (a *pop3MailstoreAdapter) Authenticate(username, password string) (bool, error) {
	return a.mailstore.Authenticate(username, password)
}

func (a *pop3MailstoreAdapter) ListMessages(user string) ([]*pop3.Message, error) {
	// Fetch messages from the INBOX mailbox
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", "1:*", []string{"RFC822.SIZE"})
	if err != nil {
		return nil, err
	}

	result := make([]*pop3.Message, 0, len(msgs))
	for i, msg := range msgs {
		result = append(result, &pop3.Message{
			Index: i,
			UID:   fmt.Sprintf("%d", msg.UID),
			Size:  msg.Size,
		})
	}
	return result, nil
}

// The pop3 package addresses messages by 1-based message number (RFC 1939
// §4), which is the INBOX sequence number; the methods below use it as is.
// They used to add 1, so RETR n read message n+1 and DELE n removed message
// n+1 (F4977).

// pop3SeqSet converts a 1-based POP3 message number to an IMAP sequence set.
func pop3SeqSet(index int) (string, error) {
	if index < 1 {
		return "", fmt.Errorf("message not found")
	}
	return fmt.Sprintf("%d", index), nil
}

func (a *pop3MailstoreAdapter) GetMessage(user string, index int) (*pop3.Message, error) {
	seq, err := pop3SeqSet(index)
	if err != nil {
		return nil, err
	}
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", seq, []string{"RFC822.SIZE"})
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	msg := msgs[0]
	return &pop3.Message{
		Index: index,
		UID:   fmt.Sprintf("%d", msg.UID),
		Size:  msg.Size,
	}, nil
}

func (a *pop3MailstoreAdapter) GetMessageData(user string, index int) ([]byte, error) {
	seq, err := pop3SeqSet(index)
	if err != nil {
		return nil, err
	}
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", seq, []string{"RFC822"})
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	// FetchMessages leaves Data nil when the blob cannot be read; returning
	// it made RETR answer "+OK 0 octets" and DELE then dropped the message
	// the client never received (F5421). A stored empty message is non-nil.
	if msgs[0].Data == nil {
		return nil, fmt.Errorf("message data unreadable")
	}
	return msgs[0].Data, nil
}

// DeleteMessage removes message number index from the maildrop. pop3 calls
// it in the UPDATE state, highest number first, and relies on the removal
// (RFC 1939 §6): flagging \Deleted alone left the message in the maildrop
// for every later session (F4977).
func (a *pop3MailstoreAdapter) DeleteMessage(user string, index int) error {
	seq, err := pop3SeqSet(index)
	if err != nil {
		return err
	}
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", seq, []string{"FLAGS"})
	if err != nil || len(msgs) == 0 {
		return fmt.Errorf("message not found")
	}
	if err := a.mailstore.StoreFlags(user, "INBOX", seq, []string{"\\Deleted"}, imap.FlagAdd); err != nil {
		return err
	}
	return a.mailstore.ExpungeUIDs(user, "INBOX", []uint32{msgs[0].UID})
}

func (a *pop3MailstoreAdapter) GetMessageCount(user string) (int, error) {
	msgs, err := a.ListMessages(user)
	if err != nil {
		return 0, err
	}
	return len(msgs), nil
}

func (a *pop3MailstoreAdapter) GetMessageSize(user string, index int) (int64, error) {
	msg, err := a.GetMessage(user, index)
	if err != nil {
		return 0, err
	}
	return msg.Size, nil
}

// indexJob represents a search indexing task.
type indexJob struct {
	email  string
	folder string // mailbox the UID belongs to; "" means INBOX
	uid    uint32
}

// runIndexWorker processes search indexing jobs.
func (s *Server) runIndexWorker() {
	defer s.wg.Done()
	for job := range s.indexWork {
		s.indexOne(job)
	}
}

// indexOne indexes one job; a panic is contained to that job so the worker
// keeps serving the queue (F6514).
func (s *Server) indexOne(job indexJob) {
	defer func() {
		if r := recover(); r != nil {
			s.logger.Error("Index worker panic", "recover", r, "email", job.email, "uid", job.uid)
		}
	}()
	{
		// UIDs are per folder: indexing a Junk or Sieve-filed UID as INBOX
		// re-indexed an unrelated INBOX message and left the delivered one
		// unsearchable (F5118).
		folder := job.folder
		if folder == "" {
			folder = "INBOX"
		}
		if err := s.searchSvc.IndexMessage(job.email, folder, job.uid); err != nil {
			s.logger.Error("Failed to index message for search", "email", job.email, "folder", folder, "uid", job.uid, "error", err)
		}
	}
}

// accountPurger implements api.AccountPurger: it removes the per-user data of
// a deleted mailbox that the api package cannot reach itself (F6649c).
type accountPurger struct{ srv *Server }

func (p accountPurger) PurgeAccount(email string) error {
	s := p.srv
	var errs []error
	if s.sieveManager != nil {
		for _, name := range s.sieveManager.ListScripts(email) {
			s.sieveManager.DeleteScript(email, name)
		}
	}
	if s.pushSvc != nil {
		for _, sub := range s.pushSvc.GetUserSubscriptions(email) {
			_ = s.pushSvc.Unsubscribe(email, sub.ID)
		}
	}
	// F6810: calendars and address books live on disk under the DAV data
	// directories, whether or not the DAV listeners are running now.
	if s.caldavServer != nil {
		if err := s.caldavServer.PurgeUser(email); err != nil {
			errs = append(errs, err)
		}
	} else if s.config != nil {
		if err := caldav.NewStorage(filepath.Join(s.config.Server.DataDir, "caldav")).PurgeUser(email); err != nil {
			errs = append(errs, err)
		}
	}
	if s.carddavServer != nil {
		if err := s.carddavServer.PurgeUser(email); err != nil {
			errs = append(errs, err)
		}
	} else if s.config != nil {
		if err := carddav.NewStorage(filepath.Join(s.config.Server.DataDir, "carddav")).PurgeUser(email); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
