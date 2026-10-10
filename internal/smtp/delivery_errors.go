package smtp

import "errors"

// ErrMailboxFull is returned (wrapped is fine) by delivery handlers when the
// recipient's quota is exhausted. The session answers it with 452 4.2.2
// rather than the generic 451 used for other local delivery failures.
var ErrMailboxFull = errors.New("mailbox full")

// deliveryReply maps a delivery handler error to the reply the session
// sends. The handler contract is a single error for the whole transaction
// (SMTP DATA/BDAT give one reply for all recipients, unlike LMTP), so a
// quota failure for any recipient answers 452 4.2.2 and the client retries;
// every other local failure is 451 4.3.0.
func deliveryReply(err error) (int, string) {
	if errors.Is(err, ErrMailboxFull) {
		return 452, "4.2.2 Mailbox full, try again later"
	}
	return 451, "4.3.0 Requested action aborted: local error in processing"
}
