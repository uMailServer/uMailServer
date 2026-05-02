package imap

import "time"

// Mailbox represents an IMAP mailbox/folder
type Mailbox struct {
	Name           string
	Exists         int
	Recent         int
	Unseen         int
	UIDValidity    uint32
	UIDNext        uint32
	HighestModSeq  uint64  // RFC 7162: highest modification sequence number
	Flags          []string
	PermanentFlags []string
	ReadOnly       bool
	HasChildren    bool // RFC 3348: mailbox has child mailboxes
	HasNoSelect    bool // RFC 3348: mailbox cannot be selected
}

// Message represents an IMAP message
type Message struct {
	SeqNum        uint32
	UID           uint32
	ModSeq        uint64  // RFC 7162: modification sequence number
	Flags         []string
	InternalDate  time.Time
	Size          int64
	Data          []byte
	Subject       string
	Date          string
	From          string
	To            string
	Envelope      *Envelope
	BodyStructure *BodyStructure
}

// Envelope represents the envelope structure of a message (RFC 3501)
type Envelope struct {
	Date      string
	Subject   string
	From      []*Address
	Sender    []*Address
	ReplyTo   []*Address
	To        []*Address
	Cc        []*Address
	Bcc       []*Address
	InReplyTo string
	MessageID string
}

// Address represents an email address (RFC 3501)
type Address struct {
	PersonalName string
	AtDomainList string
	MailboxName  string
	HostName     string
}

// BodyStructure represents the body structure of a message (RFC 3501)
type BodyStructure struct {
	Type        string
	Subtype     string
	Parameters  map[string]string
	ID          string
	Description string
	Encoding    string
	Size        int64
	Lines       int64            // for text/* types
	Parts       []*BodyStructure // for multipart
}

// SearchCriteria represents IMAP SEARCH criteria (RFC 3501)
type SearchCriteria struct {
	// Keywords
	All        bool
	Answered   bool
	Deleted    bool
	Flagged    bool
	New        bool
	Old        bool
	Recent     bool
	Seen       bool
	Unanswered bool
	Undeleted  bool
	Unflagged  bool
	Unseen     bool
	Draft      bool
	Undraft    bool

	// Sequence/UID sets
	SeqSet string
	UIDSet string

	// String searches
	From    string
	To      string
	Cc      string
	Bcc     string
	Subject string
	Body    string
	Text    string

	// Header searches
	Header map[string]string

	// Date searches
	Before     time.Time
	On         time.Time
	Since      time.Time
	SentBefore time.Time
	SentOn     time.Time
	SentSince  time.Time

	// Size searches
	Larger  int64
	Smaller int64

	// Logical operators
	Not *SearchCriteria
	Or  [2]*SearchCriteria
}

// StatusItem represents items that can be queried with STATUS command
type StatusItem string

const (
	StatusMessages    StatusItem = "MESSAGES"
	StatusRecent      StatusItem = "RECENT"
	StatusUIDNext     StatusItem = "UIDNEXT"
	StatusUIDValidity StatusItem = "UIDVALIDITY"
	StatusUnseen      StatusItem = "UNSEEN"
)
