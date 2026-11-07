package bot

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
)

// memoLinkPattern matches the memo name in the links the bot writes into its
// replies, such as [memos/abc123](https://memos.example.com/memos/abc123).
// Replies from Memos versions that used /m/ are covered too, because the name
// itself is what is parsed.
var memoLinkPattern = regexp.MustCompile(`\[(memos/[A-Za-z0-9_-]+)\]\(`)

// repliedMemoName returns the memo a message edits. It is set when the message
// replies to a message the bot sent that links a memo: the saved-memo
// confirmation and the "memo updated" confirmation both qualify. Replies to
// anything else return false, so they create a new memo as before.
func repliedMemoName(msg *models.Message) (string, bool) {
	reply := msg.ReplyToMessage
	if reply == nil || reply.From == nil || !reply.From.IsBot || reply.Text == "" {
		return "", false
	}
	match := memoLinkPattern.FindStringSubmatch(reply.Text)
	if match == nil {
		return "", false
	}
	if _, err := memos.MemoUID(match[1]); err != nil {
		return "", false
	}
	return match[1], true
}

// isDeleteReply reports a reply that deletes the memo it answers instead of
// editing it. The command has no meaning on its own, so it is not registered
// with Telegram: it only works as a reply.
func isDeleteReply(msg *models.Message) bool {
	fields := strings.Fields(strings.ToLower(msg.Text))
	if len(fields) != 1 {
		return false
	}
	return fields[0] == "/delete" || fields[0] == "/del"
}

// deleteRepliedMemo deletes the memo a /delete reply answers and confirms it in
// the chat. Deletion is irreversible, so the confirmation names the memo.
func (s *Service) deleteRepliedMemo(ctx context.Context, msg *models.Message, accessToken, name string) {
	client := s.memos.WithAccessToken(accessToken)
	link, linkErr := s.memoLink(&memosapi.Memo{Name: name})
	if err := client.DeleteMemo(ctx, name); err != nil {
		slog.Error("failed to delete memo", slog.Any("err", err), slog.String("memo", name))
		s.reply(ctx, msg.Chat.ID, "Failed to delete memo")
		return
	}
	text := "Memo deleted"
	if linkErr == nil {
		text = fmt.Sprintf("Memo %s deleted", link)
	}
	s.send(ctx, &bot.SendMessageParams{
		ChatID:              msg.Chat.ID,
		Text:                text,
		ParseMode:           models.ParseModeMarkdown,
		DisableNotification: true,
		ReplyParameters:     &models.ReplyParameters{MessageID: msg.ID},
	})
}

// editMemo rewrites the content of the memo the message replies to, so a memo
// written from Telegram can be corrected without opening the web app. The
// content goes through the same entity-to-Markdown conversion as a new memo,
// which is what makes code blocks, inline code, quotes and links editable.
func (s *Service) editMemo(ctx context.Context, msg *models.Message, accessToken, name string) {
	content := formattedContent(msg)
	if content == "" {
		s.reply(ctx, msg.Chat.ID, "Reply to a saved memo with the new content as text")
		return
	}
	client := s.memos.WithAccessToken(accessToken)
	memo, err := client.GetMemo(ctx, name)
	if err != nil {
		slog.Error("failed to load memo to edit", slog.Any("err", err), slog.String("memo", name))
		s.reply(ctx, msg.Chat.ID, fmt.Sprintf("Failed to edit %s: it is not available to you", escapeMarkdown(name)))
		return
	}
	// Only the content is replaced; visibility, pinning and space stay as they
	// are on the server, because the update mask lists the content alone.
	memo.Content = content
	updated, err := client.UpdateMemo(ctx, memo, memos.FieldContent)
	if err != nil {
		slog.Error("failed to edit memo", slog.Any("err", err), slog.String("memo", name))
		s.reply(ctx, msg.Chat.ID, "Failed to edit memo")
		return
	}
	link, err := s.memoLink(updated)
	if err != nil {
		slog.Error("failed to link memo", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Memo updated, but the link could not be built")
		return
	}
	var title string
	if defaultSpace := s.store.DefaultSpace(msg.From.ID); updated.Space != "" && updated.Space == defaultSpace.Name {
		title = defaultSpace.Title
	}
	s.send(ctx, &bot.SendMessageParams{
		ChatID:              msg.Chat.ID,
		Text:                fmt.Sprintf("Content updated as %s with %s", memoStatus(updated, title), link),
		ParseMode:           models.ParseModeMarkdown,
		DisableNotification: true,
		ReplyParameters:     &models.ReplyParameters{MessageID: msg.ID},
		ReplyMarkup:         s.memoKeyboard(updated),
	})
}
