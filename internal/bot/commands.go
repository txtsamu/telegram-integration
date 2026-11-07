package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
)

// handleStart binds the sender to a Memos Personal Access Token after
// checking that the token works.
func (s *Service) handleStart(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	accessToken := commandArgs(msg.Text, commandStart)
	if accessToken == "" {
		s.reply(ctx, msg.Chat.ID, "Usage: /start <access_token>")
		return
	}
	user, err := s.memos.WithAccessToken(accessToken).GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to verify access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Invalid access token")
		return
	}
	if err := s.store.SetAccessToken(msg.From.ID, accessToken); err != nil {
		slog.Error("failed to save access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to save access token, please try again")
		return
	}
	s.reply(ctx, msg.Chat.ID, fmt.Sprintf("Hello %s!", user.DisplayName))
}

// handleSearch replies with the sender's memos that contain the given words,
// one message per memo.
func (s *Service) handleSearch(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	query := commandArgs(msg.Text, commandSearch)
	if query == "" {
		s.reply(ctx, msg.Chat.ID, "Usage: /search <words>")
		return
	}
	accessToken, ok := s.store.AccessToken(msg.From.ID)
	if !ok {
		s.reply(ctx, msg.Chat.ID, startFirst)
		return
	}
	client := s.memos.WithAccessToken(accessToken)
	user, err := client.GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to verify access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Invalid access token")
		return
	}
	found, err := client.ListMemos(ctx, memos.SearchFilter(query, user))
	if err != nil {
		slog.Error("failed to search memos", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to search memos")
		return
	}
	if len(found) == 0 {
		s.reply(ctx, msg.Chat.ID, "No memos found for the specified search criteria.")
		return
	}
	for _, memo := range found {
		// Telegram rejects a message longer than 4096 characters, so a long
		// memo is cut instead of failing the whole reply.
		s.reply(ctx, msg.Chat.ID, truncate(memo.Name+"\n"+memo.Content, maxTelegramText))
	}
}

// listLimit is how many of the sender's memos /list shows, because each one is
// a separate message with its own buttons.
const listLimit = 5

// handleList posts the sender's most recent memos, one message each, with the
// buttons that act on them. Replying to any of those messages edits it, and its
// Delete button removes it.
func (s *Service) handleList(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	accessToken, ok := s.store.AccessToken(msg.From.ID)
	if !ok {
		s.reply(ctx, msg.Chat.ID, startFirst)
		return
	}
	client := s.memos.WithAccessToken(accessToken)
	user, err := client.GetCurrentUser(ctx)
	if err != nil {
		slog.Warn("failed to verify access token", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Invalid access token")
		return
	}
	filter := memos.CreatorFilter(user)
	if filter == "" {
		s.reply(ctx, msg.Chat.ID, "Failed to list memos")
		return
	}
	found, err := client.ListMemos(ctx, filter)
	if err != nil {
		slog.Error("failed to list memos", slog.Any("err", err))
		s.reply(ctx, msg.Chat.ID, "Failed to list memos")
		return
	}
	if len(found) == 0 {
		s.reply(ctx, msg.Chat.ID, "You have no memos yet. Send me a message to save one.")
		return
	}
	if len(found) > listLimit {
		found = found[:listLimit]
	}
	for _, memo := range found {
		s.sendMemoCard(ctx, msg.Chat.ID, memo)
	}
}

// sendMemoCard posts one memo as its own message: a short preview, a link, and
// the buttons for visibility, pinning, spaces and deletion.
func (s *Service) sendMemoCard(ctx context.Context, chatID int64, memo memosapi.Memo) {
	link, err := s.memoLink(&memo)
	if err != nil {
		slog.Error("failed to link memo", slog.Any("err", err), slog.String("memo", memo.Name))
		return
	}
	text := link
	if preview := memoPreview(memo); preview != "" {
		text = escapeMarkdown(preview) + "\n" + link
	}
	s.send(ctx, &bot.SendMessageParams{
		ChatID:              chatID,
		Text:                text,
		ParseMode:           models.ParseModeMarkdown,
		DisableNotification: true,
		ReplyMarkup:         s.memoKeyboard(&memo),
	})
}

// memoPreview returns the first line of a memo, shortened, so that a list of
// memos can be told apart in Telegram.
func memoPreview(memo memosapi.Memo) string {
	text := strings.TrimSpace(memo.Content)
	if text == "" {
		text = strings.TrimSpace(memo.Property.Title)
	}
	if text == "" {
		return ""
	}
	if line, _, found := strings.Cut(text, "\n"); found {
		text = line
	}
	return truncate(text, memoPreviewLimit)
}
