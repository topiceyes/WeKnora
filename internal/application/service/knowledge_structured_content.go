package service

import (
	"fmt"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	secutils "github.com/Tencent/WeKnora/internal/utils"
)

// Never trust a separate client Markdown string when the canonical structure is
// present: search, export and resource binding must describe the actual body.
func prepareEditorContent(markdown string, document *types.StructuredDocument) (string, error) {
	if document != nil {
		if err := document.Validate(); err != nil {
			return "", werrors.NewValidationError(fmt.Sprintf("无效的结构化正文: %v", err))
		}
		markdown = document.Markdown()
	}
	return secutils.CleanMarkdown(markdown), nil
}
