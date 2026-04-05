package jmap

// Core JMAP types per RFC 8620

// Request represents a JMAP request
type Request struct {
	Using         []string      `json:"using"`
	MethodCalls   []MethodCall  `json:"methodCalls"`
	CreatedIDs    map[string]string `json:"createdIds,omitempty"`
}

// MethodCall represents a single method call
type MethodCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
	ID   string                 `json:"id"`
}

// Response represents a method response
type Response struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
	ID   string                 `json:"id"`
}

// ResponseObject represents the top-level response
type ResponseObject struct {
	SessionState    string     `json:"sessionState"`
	MethodResponses []Response `json:"methodResponses"`
}

// SessionResponse represents a session object
type SessionResponse struct {
	Capabilities     map[string]interface{} `json:"capabilities"`
	Accounts         map[string]Account     `json:"accounts"`
	PrimaryAccounts  map[string]string      `json:"primaryAccounts"`
	Username         string                 `json:"username"`
	APIURL           string                 `json:"apiUrl"`
	DownloadURL      string                 `json:"downloadUrl"`
	UploadURL        string                 `json:"uploadUrl"`
	EventSourceURL   string                 `json:"eventSourceUrl"`
	State            string                 `json:"state"`
}

// Account represents a JMAP account
type Account struct {
	Name                string                 `json:"name"`
	IsPrimary           bool                   `json:"isPrimary"`
	AccountCapabilities map[string]interface{} `json:"accountCapabilities"`
}

// CoreCapabilities represents core JMAP capabilities
type CoreCapabilities struct {
	MaxSizeUpload           int      `json:"maxSizeUpload"`
	MaxConcurrentUpload     int      `json:"maxConcurrentUpload"`
	MaxSizeRequest          int      `json:"maxSizeRequest"`
	MaxConcurrentRequests   int      `json:"maxConcurrentRequests"`
	MaxCallsInRequest       int      `json:"maxCallsInRequest"`
	MaxObjectsInGet         int      `json:"maxObjectsInGet"`
	MaxObjectsInSet         int      `json:"maxObjectsInSet"`
	CollationAlgorithms     []string `json:"collationAlgorithms"`
}

// MailCapabilities represents mail JMAP capabilities
type MailCapabilities struct {
	MaxMailboxesPerEmail       int      `json:"maxMailboxesPerEmail"`
	MaxMailboxDepth            int      `json:"maxMailboxDepth"`
	MaxSizeMailboxName         int      `json:"maxSizeMailboxName"`
	MaxSizeAttachmentsPerEmail int64    `json:"maxSizeAttachmentsPerEmail"`
	EmailQuerySortOptions      []string `json:"emailQuerySortOptions"`
}

// UploadResponse represents an upload response
type UploadResponse struct {
	AccountID string `json:"accountId"`
	BlobID    string `json:"blobId"`
	Type      string `json:"type"`
	Size      int    `json:"size"`
}

// Mailbox represents a mailbox (RFC 8621)
type Mailbox struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
	Role     string `json:"role,omitempty"`
	SortOrder int   `json:"sortOrder,omitempty"`

	// Counters
	TotalEmails        int `json:"totalEmails"`
	UnreadEmails       int `json:"unreadEmails"`
	TotalThreads       int `json:"totalThreads"`
	UnreadThreads      int `json:"unreadThreads"`

	// Permissions
	MyRights  MailboxRights `json:"myRights"`
	IsSubscribed bool       `json:"isSubscribed"`
}

// MailboxRights represents mailbox ACL rights
type MailboxRights struct {
	MayReadItems      bool `json:"mayReadItems"`
	MayAddItems       bool `json:"mayAddItems"`
	MayRemoveItems    bool `json:"mayRemoveItems"`
	MaySetSeen        bool `json:"maySetSeen"`
	MaySetKeywords    bool `json:"maySetKeywords"`
	MayCreateChild    bool `json:"mayCreateChild"`
	MayRename         bool `json:"mayRename"`
	MayDelete         bool `json:"mayDelete"`
	MaySubmit         bool `json:"maySubmit"`
}

// Email represents an email (RFC 8621)
type Email struct {
	ID          string            `json:"id"`
	BlobID      string            `json:"blobId"`
	ThreadID    string            `json:"threadId"`
	MailboxIDs  map[string]bool   `json:"mailboxIds"`
	Keywords    map[string]bool   `json:"keywords,omitempty"`
	Size        int64             `json:"size"`

	// Headers
	ReceivedAt  string            `json:"receivedAt"`

	// Message properties
	MessageID   []string          `json:"messageId,omitempty"`
	InReplyTo   []string          `json:"inReplyTo,omitempty"`
	References  []string          `json:"references,omitempty"`
	Sender      []EmailAddress    `json:"sender,omitempty"`
	From        []EmailAddress    `json:"from,omitempty"`
	To          []EmailAddress    `json:"to,omitempty"`
	CC          []EmailAddress    `json:"cc,omitempty"`
	BCC         []EmailAddress    `json:"bcc,omitempty"`
	ReplyTo     []EmailAddress    `json:"replyTo,omitempty"`
	Subject     string            `json:"subject,omitempty"`
	SentAt      string            `json:"sentAt,omitempty"`

	// Body
	BodyStructure *EmailBodyPart  `json:"bodyStructure,omitempty"`
	BodyValues  map[string]EmailBodyValue `json:"bodyValues,omitempty"`
	TextBody    []EmailBodyPart   `json:"textBody,omitempty"`
	HTMLBody    []EmailBodyPart   `json:"htmlBody,omitempty"`
	Attachments []EmailBodyPart   `json:"attachments,omitempty"`

	// Preview
	Preview     string            `json:"preview,omitempty"`
	HasAttachment bool            `json:"hasAttachment,omitempty"`
}

// EmailAddress represents an email address
type EmailAddress struct {
	Name    string `json:"name,omitempty"`
	Email   string `json:"email"`
}

// EmailBodyPart represents an email body part
type EmailBodyPart struct {
	PartID       string                 `json:"partId,omitempty"`
	BlobID       string                 `json:"blobId,omitempty"`
	Size         int64                  `json:"size,omitempty"`
	Name         string                 `json:"name,omitempty"`
	Type         string                 `json:"type"`
	Charset      string                 `json:"charset,omitempty"`
	Disposition  string                 `json:"disposition,omitempty"`
	CID          string                 `json:"cid,omitempty"`
	Language     []string               `json:"language,omitempty"`
	Location     string                 `json:"location,omitempty"`
	SubParts     []EmailBodyPart        `json:"subParts,omitempty"`
	Headers      map[string]string      `json:"headers,omitempty"`
}

// EmailBodyValue represents a fetched body value
type EmailBodyValue struct {
	Value        string `json:"value"`
	IsTruncated  bool   `json:"isTruncated,omitempty"`
	IsEncodingProblem bool `json:"isEncodingProblem,omitempty"`
}

// Thread represents a conversation thread
type Thread struct {
	ID       string   `json:"id"`
	EmailIDs []string `json:"emailIds"`
}

// Identity represents an email identity for sending
type Identity struct {
	ID           string   `json:"id"`
	Name         string   `json:"name,omitempty"`
	Email        string   `json:"email"`
	ReplyTo      []EmailAddress `json:"replyTo,omitempty"`
	Bcc          []EmailAddress `json:"bcc,omitempty"`
	TextSignature string  `json:"textSignature,omitempty"`
	HTMLSignature string  `json:"htmlSignature,omitempty"`
	MayDelete    bool     `json:"mayDelete"`
}

// SearchSnippet represents search result snippets
type SearchSnippet struct {
	EmailID string `json:"emailId"`
	Subject string `json:"subject,omitempty"`
	Preview string `json:"preview,omitempty"`
}

// FilterCondition represents a query filter
type FilterCondition struct {
	InMailbox       string            `json:"inMailbox,omitempty"`
	InMailboxOtherThan []string       `json:"inMailboxOtherThan,omitempty"`
	Before          string            `json:"before,omitempty"`
	After           string            `json:"after,omitempty"`
	MinSize         int64             `json:"minSize,omitempty"`
	MaxSize         int64             `json:"maxSize,omitempty"`
	AllInThreadHaveKeyword string     `json:"allInThreadHaveKeyword,omitempty"`
	SomeInThreadHaveKeyword string    `json:"someInThreadHaveKeyword,omitempty"`
	NoneInThreadHaveKeyword string    `json:"noneInThreadHaveKeyword,omitempty"`
	HasKeyword      string            `json:"hasKeyword,omitempty"`
	NotKeyword      string            `json:"notKeyword,omitempty"`
	HasAttachment   bool              `json:"hasAttachment,omitempty"`
	Text            string            `json:"text,omitempty"`
	From            string            `json:"from,omitempty"`
	To              string            `json:"to,omitempty"`
	Cc              string            `json:"cc,omitempty"`
	Bcc             string            `json:"bcc,omitempty"`
	Subject         string            `json:"subject,omitempty"`
	Body            string            `json:"body,omitempty"`
	Header          []string          `json:"header,omitempty"`
}

// Comparator represents a sort comparator
type Comparator struct {
	Property  string `json:"property"`
	IsAscending bool `json:"isAscending,omitempty"`
}

// GetRequest represents a /get request
type GetRequest struct {
	AccountID string   `json:"accountId"`
	IDs       []string `json:"ids,omitempty"`
	Properties []string `json:"properties,omitempty"`
}

// GetResponse represents a /get response
type GetResponse struct {
	AccountID string      `json:"accountId"`
	State     string      `json:"state"`
	List      interface{} `json:"list"`
	NotFound  []string    `json:"notFound,omitempty"`
}

// QueryRequest represents a /query request
type QueryRequest struct {
	AccountID       string          `json:"accountId"`
	Filter          interface{}     `json:"filter,omitempty"`
	Sort            []Comparator    `json:"sort,omitempty"`
	Position        int             `json:"position,omitempty"`
	Anchor          string          `json:"anchor,omitempty"`
	AnchorOffset    int             `json:"anchorOffset,omitempty"`
	Limit           int             `json:"limit,omitempty"`
	CalculateTotal  bool            `json:"calculateTotal,omitempty"`
}

// QueryResponse represents a /query response
type QueryResponse struct {
	AccountID  string   `json:"accountId"`
	QueryState string   `json:"queryState"`
	CanCalculateChanges bool `json:"canCalculateChanges"`
	Position   int      `json:"position"`
	Total      int      `json:"total,omitempty"`
	IDs        []string `json:"ids"`
}

// SetRequest represents a /set request
type SetRequest struct {
	AccountID  string                 `json:"accountId"`
	IfInState  string                 `json:"ifInState,omitempty"`
	Create     map[string]interface{} `json:"create,omitempty"`
	Update     map[string]interface{} `json:"update,omitempty"`
	Destroy    []string               `json:"destroy,omitempty"`
}

// SetResponse represents a /set response
type SetResponse struct {
	AccountID      string                 `json:"accountId"`
	OldState       string                 `json:"oldState,omitempty"`
	NewState       string                 `json:"newState"`
	Created        map[string]interface{} `json:"created,omitempty"`
	Updated        map[string]interface{} `json:"updated,omitempty"`
	Destroyed      []string               `json:"destroyed,omitempty"`
	NotCreated     map[string]interface{} `json:"notCreated,omitempty"`
	NotUpdated     map[string]interface{} `json:"notUpdated,omitempty"`
	NotDestroyed   map[string]interface{} `json:"notDestroyed,omitempty"`
}

// ImportRequest represents an /import request
type ImportRequest struct {
	AccountID string                 `json:"accountId"`
	Emails    map[string]EmailImport `json:"emails"`
}

// EmailImport represents a single email import
type EmailImport struct {
	BlobID     string          `json:"blobId"`
	MailboxIDs map[string]bool `json:"mailboxIds"`
	Keywords   map[string]bool `json:"keywords,omitempty"`
	ReceivedAt string          `json:"receivedAt,omitempty"`
}

// ImportResponse represents an /import response
type ImportResponse struct {
	AccountID  string                 `json:"accountId"`
	OldState   string                 `json:"oldState,omitempty"`
	NewState   string                 `json:"newState"`
	Created    map[string]Email       `json:"created,omitempty"`
	NotCreated map[string]interface{} `json:"notCreated,omitempty"`
}
