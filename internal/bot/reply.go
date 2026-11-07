package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

const startFirst = "Please start the bot with /start <access_token>"

const (
	// maxTelegramText is the longest text Telegram accepts in one message.
	maxTelegramText = 4096
	// memoPreviewLimit is how much of a memo /list shows above its link.
	memoPreviewLimit = 80
)

// The helpers below log Telegram failures, which are otherwise invisible to
// both the user and the operator.

func (s *Service) reply(ctx context.Context, chatID int64, text string) {
	s.send(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
}

func (s *Service) send(ctx context.Context, params *bot.SendMessageParams) {
	if _, err := s.bot.SendMessage(ctx, params); err != nil {
		slog.Error("failed to send Telegram message", slog.Any("err", redactTelegramError(err)))
	}
}

// edit replaces a message's text and buttons. It returns the error so a caller
// can tell the user the message could not be changed: a swallowed error made a
// failed button press look like nothing happened at all.
func (s *Service) edit(ctx context.Context, params *bot.EditMessageTextParams) error {
	if _, err := s.bot.EditMessageText(ctx, params); err != nil {
		err = redactTelegramError(err)
		slog.Error("failed to edit Telegram message", slog.Any("err", err))
		return err
	}
	return nil
}

// editMarkup replaces a message's buttons.
func (s *Service) editMarkup(ctx context.Context, msg *models.Message, keyboard *models.InlineKeyboardMarkup) {
	if _, err := s.bot.EditMessageReplyMarkup(ctx, &bot.EditMessageReplyMarkupParams{ChatID: msg.Chat.ID, MessageID: msg.ID, ReplyMarkup: keyboard}); err != nil {
		slog.Error("failed to edit Telegram buttons", slog.Any("err", redactTelegramError(err)))
	}
}

// answer acknowledges a button press with a brief notification.
func (s *Service) answer(ctx context.Context, queryID, text string) {
	s.answerCallback(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: queryID, Text: text})
}

// alert acknowledges a button press with a dialog the user must dismiss.
func (s *Service) alert(ctx context.Context, queryID, text string) {
	s.answerCallback(ctx, &bot.AnswerCallbackQueryParams{CallbackQueryID: queryID, Text: text, ShowAlert: true})
}

func (s *Service) answerCallback(ctx context.Context, params *bot.AnswerCallbackQueryParams) {
	if _, err := s.bot.AnswerCallbackQuery(ctx, params); err != nil {
		slog.Error("failed to answer Telegram callback", slog.Any("err", redactTelegramError(err)))
	}
}

// markdownEscaper escapes every character MarkdownV2 reserves, for any text the
// bot writes itself or copies from a memo. ParseModeMarkdown is MarkdownV2 in
// go-telegram/bot, which rejects a message with a single unescaped reserved
// character, so literal text and separator punctuation must always go through
// it. Rendered links are built separately and passed unescaped.
var markdownEscaper = strings.NewReplacer(
	"\\", `\\`,
	"_", `\_`,
	"*", `\*`,
	"`", "\\`",
	"[", `\[`,
	"]", `\]`,
	"(", `\(`,
	")", `\)`,
	"~", `\~`,
	">", `\>`,
	"#", `\#`,
	"+", `\+`,
	"-", `\-`,
	"=", `\=`,
	"|", `\|`,
	"{", `\{`,
	"}", `\}`,
	".", `\.`,
	"!", `\!`,
)

func escapeMarkdown(text string) string {
	return markdownEscaper.Replace(text)
}

// truncate shortens text to at most limit characters, marking the cut with an
// ellipsis, which is not a character MarkdownV2 reserves.
func truncate(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit || limit < 2 {
		return text
	}
	return string(runes[:limit-1]) + "…"
}

// redactTelegramError drops request URLs, which contain the bot token, from
// transport errors so that they can be logged safely.
func redactTelegramError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("%s request failed: %w", urlErr.Op, urlErr.Err)
	}
	return err
}
