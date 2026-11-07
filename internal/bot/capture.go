package bot

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
	"github.com/usememos/memogram/internal/store"
)

// handleMessage saves a message that is not a command as a memo, in the
// sender's default space if they chose one, uploads its file, and replies
// with a link and the memo's buttons.
func (s *Service) handleMessage(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	if msg == nil || msg.From == nil {
		return
	}
	accessToken, ok := s.store.AccessToken(msg.From.ID)
	if !ok {
		s.reply(ctx, msg.Chat.ID, startFirst)
		return
	}
	// A reply to the bot's own saved-memo message edits that memo, or deletes it
	// when the reply is the /delete command; any other message, including a
	// reply to someone else, creates a new one.
	if name, ok := repliedMemoName(msg); ok {
		if isDeleteReply(msg) {
			s.deleteRepliedMemo(ctx, msg, accessToken, name)
			return
		}
		s.editMemo(ctx, msg, accessToken, name)
		return
	}
	content := messageContent(msg)
	file, hasFile := messageFile(msg)
	// A Markdown document becomes the memo body, so its text renders in Memos
	// instead of sitting behind an attachment. Other documents are attached.
	attachFile := hasFile
	if hasFile && isMarkdownDocument(file) {
		text, err := s.readMarkdownFile(ctx, file)
		switch {
		case err != nil:
			slog.Warn("markdown file attached instead of saved as content", slog.Any("err", err))
		case text == "":
			// An empty document has no content to add, so it is attached.
		default:
			if content == "" {
				content = text
			} else {
				content += "\n\n" + text
			}
			attachFile = false
		}
	}
	if content == "" && !hasFile {
		s.reply(ctx, msg.Chat.ID, "Please input memo content")
		return
	}

	client := s.memos.WithAccessToken(accessToken)
	memo, lostSpace, err := s.createMemo(ctx, client, msg, content)
	if err != nil {
		slog.Error("failed to create memo", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to create memo")
		return
	}
	attachmentFailed := false
	if attachFile {
		if err := s.saveAttachment(ctx, client, file, memo); err != nil {
			attachmentFailed = true
			slog.Error("attachment upload failed", slog.Any("err", err))
		}
	}

	link, err := s.memoLink(memo)
	if err != nil {
		slog.Error("failed to link memo", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to save memo")
		return
	}
	var title string
	if defaultSpace := s.store.DefaultSpace(msg.From.ID); memo.Space != "" && memo.Space == defaultSpace.Name {
		title = defaultSpace.Title
	}
	text := fmt.Sprintf("Content saved as %s with %s", memoStatus(memo, title), link)
	if attachmentFailed {
		text = escapeMarkdown("Memo saved, but the attachment failed to upload.") + " " + link
	}
	if lostSpace.Name != "" {
		text += "\n" + escapeMarkdown("Your default space") + " " + escapeMarkdown(storedTitle(lostSpace)) +
			escapeMarkdown(" is no longer available, so this memo was saved without it. Use /space to choose another.")
	}
	s.send(ctx, &bot.SendMessageParams{
		ChatID:              msg.Chat.ID,
		Text:                text,
		ParseMode:           models.ParseModeMarkdown,
		DisableNotification: true,
		ReplyParameters:     &models.ReplyParameters{MessageID: msg.ID},
		ReplyMarkup:         s.memoKeyboard(memo),
	})
}

// createMemo creates the memo for a message. All parts of an album share the
// memo created for the first part to arrive, which carries the album caption.
// It also returns the sender's default space if the server rejected it, which
// only the part that created the memo reports.
func (s *Service) createMemo(ctx context.Context, client *memos.Client, msg *models.Message, content string) (*memosapi.Memo, store.Space, error) {
	var lostSpace store.Space
	// albums.memo calls create on this goroutine, if at all.
	create := func() (memo *memosapi.Memo, err error) {
		memo, lostSpace, err = s.createInDefaultSpace(ctx, client, msg.From.ID, content)
		return memo, err
	}
	if msg.MediaGroupID == "" {
		memo, err := create()
		return memo, lostSpace, err
	}
	memo, err := s.albums.memo(ctx, fmt.Sprintf("%d:%s", msg.Chat.ID, msg.MediaGroupID), create)
	return memo, lostSpace, err
}
