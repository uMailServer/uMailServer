package imap

import (
	"reflect"
	"testing"
	"time"
)

const r295User = "copy-user@example.com"

const r295Reply = "From: b@y.test\r\nTo: a@x.test\r\nSubject: Re: Plan\r\nMessage-ID: <r1@y.test>\r\nIn-Reply-To: <root@x.test>\r\nReferences: <root@x.test>\r\n\r\nreply\r\n"

func r295Store(t *testing.T) *BboltMailstore {
	t.Helper()
	ms, err := NewBboltMailstore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ms.Close() })
	for _, mb := range []string{"INBOX", "Archive"} {
		if err := ms.CreateMailbox(r295User, mb); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.AppendMessage(r295User, "INBOX", nil, time.Now(), []byte(r295Reply)); err != nil {
		t.Fatal(err)
	}
	return ms
}

// F7691: COPY and MOVE keep the message's conversation identity.
func TestCopyKeepsThreadIdentity(t *testing.T) {
	ms := r295Store(t)
	src, err := ms.db.GetMessageMetadata(r295User, "INBOX", 1)
	if err != nil || src.ThreadID == "" {
		t.Fatalf("setup: APPEND must assign a thread id: %+v %v", src, err)
	}
	_, _, dstUIDs, err := ms.CopyMessagesUIDs(r295User, "INBOX", "Archive", "1")
	if err != nil || len(dstUIDs) != 1 {
		t.Fatalf("copy: %v %v", dstUIDs, err)
	}
	dst, err := ms.db.GetMessageMetadata(r295User, "Archive", dstUIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if dst.ThreadID != src.ThreadID || dst.InReplyTo != src.InReplyTo || !reflect.DeepEqual(dst.References, src.References) || dst.IsThreadRoot != src.IsThreadRoot {
		t.Fatalf("copy lost the conversation: src=%+v dst=%+v", src, dst)
	}
}

func TestMoveKeepsThreadIdentity(t *testing.T) {
	ms := r295Store(t)
	src, _ := ms.db.GetMessageMetadata(r295User, "INBOX", 1)
	_, _, dstUIDs, err := ms.MoveMessagesUIDs(r295User, "INBOX", "Archive", "1")
	if err != nil || len(dstUIDs) != 1 {
		t.Fatalf("move: %v %v", dstUIDs, err)
	}
	dst, err := ms.db.GetMessageMetadata(r295User, "Archive", dstUIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if dst.ThreadID != src.ThreadID || dst.InReplyTo != src.InReplyTo {
		t.Fatalf("move lost the conversation: src=%+v dst=%+v", src, dst)
	}
}
