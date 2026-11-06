package service

import (
	"errors"

	"github.com/Tencent/WeKnora/internal/kbmutation"
)

// Transfers already require a shared local-storage volume. Keep the lock on that
// volume for the entire copy, transaction and cleanup, including across processes.
func (s *knowledgeBaseService) withWorkspaceTransferLock(kbID string, fn func() error) error {
	// Acquire the deployment-wide gate first. It drains already admitted
	// writes and also protects batches whose KB IDs are not yet resolved.
	globalRelease, err := s.mutations.Exclusive()
	if errors.Is(err, kbmutation.ErrBusy) {
		return ErrKBWorkspaceTransferInProgress
	}
	if err != nil {
		return ErrKBWorkspaceTransferUnsupported
	}
	defer globalRelease()
	release, err := kbmutation.Acquire(kbWorkspaceTransferBaseDir(), kbID, true)
	if errors.Is(err, kbmutation.ErrBusy) {
		return ErrKBWorkspaceTransferInProgress
	}
	if errors.Is(err, kbmutation.ErrUnsupported) {
		return ErrKBWorkspaceTransferUnsupported
	}
	if err != nil {
		return err
	}
	defer release()
	return fn()
}
