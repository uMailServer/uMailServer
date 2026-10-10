package smtp

import "errors"

// ErrMailboxFull is returned (wrapped is fine) by delivery handlers when the
// recipient's quota is exhausted. The session answers it with 452 4.2.2
// rather than the generic 451 used for other local delivery failures.
var ErrMailboxFull = errors.New("mailbox full")
