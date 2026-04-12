package server

import (
	"fmt"

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

func (a *pop3MailstoreAdapter) GetMessage(user string, index int) (*pop3.Message, error) {
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", fmt.Sprintf("%d", index+1), []string{"RFC822.SIZE"})
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
	msgs, err := a.mailstore.FetchMessages(user, "INBOX", fmt.Sprintf("%d", index+1), []string{"RFC822"})
	if err != nil || len(msgs) == 0 {
		return nil, fmt.Errorf("message not found")
	}
	return msgs[0].Data, nil
}

func (a *pop3MailstoreAdapter) DeleteMessage(user string, index int) error {
	seqSet := fmt.Sprintf("%d", index+1)
	return a.mailstore.StoreFlags(user, "INBOX", seqSet, []string{"\\Deleted"}, imap.FlagAdd)
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
	email string
	uid   uint32
}

// runIndexWorker processes search indexing jobs.
func (s *Server) runIndexWorker() {
	defer s.wg.Done()
	for job := range s.indexWork {
		if err := s.searchSvc.IndexMessage(job.email, "INBOX", job.uid); err != nil {
			s.logger.Error("Failed to index message for search", "email", job.email, "uid", job.uid, "error", err)
		}
	}
}
