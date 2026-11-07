package bot

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/go-telegram/bot"
)

// maxMarkdownBytes caps the text a Markdown document may add to a memo. A
// larger document is attached as a file instead, which is what happened for
// every document before Markdown files were read.
const maxMarkdownBytes = 1 << 20

var errUnreadableMarkdown = errors.New("file is not UTF-8 text")

// markdownExtensions are the file name endings whose content becomes the memo
// body.
var markdownExtensions = map[string]bool{
	".md":       true,
	".markdown": true,
	".mdown":    true,
	".mkd":      true,
	".mdx":      true,
}

// markdownMIMETypes are the content types whose content becomes the memo body,
// for documents sent without a dependable file name.
var markdownMIMETypes = map[string]bool{
	"text/markdown":   true,
	"text/x-markdown": true,
}

// isMarkdownDocument reports whether a file should be saved as memo content.
func isMarkdownDocument(file telegramFile) bool {
	if markdownMIMETypes[strings.ToLower(strings.TrimSpace(file.mimeType))] {
		return true
	}
	return markdownExtensions[strings.ToLower(path.Ext(file.name))]
}

// readMarkdownFile downloads a Markdown document and returns its text, so that
// the document becomes the memo body instead of only an attachment. It fails
// for oversized or non-UTF-8 files, which are then attached as usual.
func (s *Service) readMarkdownFile(ctx context.Context, file telegramFile) (string, error) {
	info, err := s.bot.GetFile(ctx, &bot.GetFileParams{FileID: file.id})
	if err != nil {
		return "", fmt.Errorf("get Telegram file: %w", redactTelegramError(err))
	}
	content, _, err := s.download(ctx, info)
	if err != nil {
		return "", err
	}
	if len(content) > maxMarkdownBytes {
		return "", fmt.Errorf("markdown file exceeds %d bytes", maxMarkdownBytes)
	}
	if !utf8.Valid(content) {
		return "", errUnreadableMarkdown
	}
	return strings.TrimSpace(string(content)), nil
}
