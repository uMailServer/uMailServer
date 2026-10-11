package api

import (
	"errors"
	"fmt"

	"github.com/umailserver/umailserver/internal/storage"
)

// SetQuotaAdjustFunc installs the account quota counter hook (the same
// db.IncrementQuota mapping the IMAP and JMAP servers use). Accounting is by
// blob bytes: a blob is charged once when its first row is created and
// released when its file is actually removed (F6680).
func (h *MailHandler) SetQuotaAdjustFunc(f func(user string, delta int64) error) {
	h.quotaAdjust = f
}

// chargeNewBlob reserves the blob's bytes unless a metadata row already
// references it (that row's creator paid). When the reservation is refused the
// freshly stored blob is removed again and the error is returned.
func (h *MailHandler) chargeNewBlob(user, hash string, size int64) error {
	if h.quotaAdjust == nil || h.mailDB == nil || h.msgStore == nil || size <= 0 {
		return nil
	}
	if h.countHash(user, hash) > 0 {
		return nil
	}
	if err := h.quotaAdjust(user, size); err != nil {
		_ = h.msgStore.DeleteMessage(user, hash)
		return err
	}
	return nil
}

// removeBlob deletes the blob if no row references it any more and releases
// its bytes from the quota counter only when the file was really removed.
func (h *MailHandler) removeBlob(user, hash string) {
	size, serr := h.msgStore.MessageSize(user, hash)
	err := h.msgStore.DeleteMessage(user, hash)
	if err == nil {
		if h.quotaAdjust != nil && serr == nil && size > 0 {
			_ = h.quotaAdjust(user, -size)
		}
		return
	}
	if !errors.Is(err, storage.ErrBlobReferenced) {
		// Log but don't fail - the file might already be gone.
		fmt.Printf("Warning: failed to delete message file: %v\n", err)
	}
}
