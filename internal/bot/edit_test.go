package bot

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memos"
	"github.com/usememos/memogram/internal/memosapi"
)

const savedMemoReply = "Content saved as PRIVATE with [memos/abc](http://memos.test/memos/abc)"

func botMessage(text string) *models.Message {
	return &models.Message{ID: 2, From: &models.User{ID: 9, IsBot: true}, Text: text}
}

func TestRepliedMemoName(t *testing.T) {
	for name, tc := range map[string]struct {
		reply *models.Message
		want  string
		ok    bool
	}{
		"saved memo confirmation":  {botMessage(savedMemoReply), "memos/abc", true},
		"updated memo confirmation": {botMessage("Content updated as PUBLIC with [memos/def](http://memos.test/memos/def)"), "memos/def", true},
		"bot message without link": {botMessage("Please start the bot with /start <access_token>"), "", false},
		"human message with link":  {&models.Message{ID: 2, From: &models.User{ID: 5}, Text: savedMemoReply}, "", false},
		"reply without a sender":   {&models.Message{ID: 2, Text: savedMemoReply}, "", false},
		"no reply":                 {nil, "", false},
	} {
		t.Run(name, func(t *testing.T) {
			msg := &models.Message{ID: 1}
			msg.ReplyToMessage = tc.reply
			got, ok := repliedMemoName(msg)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestEditMemoRewritesContent(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	var patched memosapi.Memo
	var mask string
	s := newTestService(t, tg, Options{
		Store: data,
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"GET /api/memos/abc": respond(`{"name":"memos/abc","content":"old","visibility":"PUBLIC","pinned":true,"state":"NORMAL"}`),
			"PATCH /api/memos/abc": func(w http.ResponseWriter, r *http.Request) {
				mask = r.URL.Query().Get("updateMask")
				if err := json.NewDecoder(r.Body).Decode(&patched); err != nil {
					t.Error(err)
				}
				patched.Name = "memos/abc"
				_ = json.NewEncoder(w).Encode(patched)
			},
		}),
	})

	content := "fixed title\nand a code block"
	msg := &models.Message{
		ID:             5,
		Chat:           models.Chat{ID: 1, Type: models.ChatTypePrivate},
		From:           &models.User{ID: 1},
		Text:           content,
		ReplyToMessage: botMessage(savedMemoReply),
		// "and a code block" is a pre entity tagged as shell.
		Entities: []models.MessageEntity{{Type: models.MessageEntityTypePre, Offset: 12, Length: 16, Language: "sh"}},
	}
	s.process(&models.Update{Message: msg})

	if want := "fixed title\n```sh\nand a code block\n```"; patched.Content != want {
		t.Fatalf("saved content %q, want %q", patched.Content, want)
	}
	if mask != memos.FieldContent {
		t.Fatalf("update mask %q, want %q", mask, memos.FieldContent)
	}
	// The edit must not disturb the fields it does not touch.
	if patched.Visibility != memosapi.PUBLIC || !patched.Pinned {
		t.Fatalf("existing fields lost: %+v", patched)
	}
	calls := tg.snapshot()
	if len(calls) != 1 || calls[0].method != "sendMessage" {
		t.Fatalf("calls %+v, want one sendMessage", calls)
	}
	if want := "Content updated as PUBLIC with [memos/abc](" + s.memos.BaseURL() + "/memos/abc)"; calls[0].text != want {
		t.Fatalf("reply %q, want %q", calls[0].text, want)
	}
	if cards := keyboardLayout(&calls[0].keyboard); !strings.Contains(cards, "Public=public memos/abc") {
		t.Fatalf("keyboard %q does not act on the edited memo", cards)
	}
}

func TestEditMemoReportsMissingMemo(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{
		Store: data,
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"GET /api/memos/missing": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
		}),
	})
	msg := &models.Message{
		ID:             5,
		Chat:           models.Chat{ID: 1, Type: models.ChatTypePrivate},
		From:           &models.User{ID: 1},
		Text:           "new text",
		ReplyToMessage: botMessage("Content saved as PRIVATE with [memos/missing](http://memos.test/memos/missing)"),
	}
	s.process(&models.Update{Message: msg})
	if calls := tg.snapshot(); len(calls) != 1 || !strings.Contains(calls[0].text, "Failed to edit memos/missing") {
		t.Fatalf("unexpected replies: %+v", calls)
	}
}

func TestReplyToAPersonStillCreatesMemo(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{
		Store: data,
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"POST /api/memos": respond(`{"name":"memos/new","content":"reply text","visibility":"PRIVATE","state":"NORMAL"}`),
		}),
	})
	msg := &models.Message{
		ID:             5,
		Chat:           models.Chat{ID: 1, Type: models.ChatTypePrivate},
		From:           &models.User{ID: 1},
		Text:           "reply text",
		ReplyToMessage: &models.Message{ID: 2, From: &models.User{ID: 7}, Text: "a friend's message"},
	}
	s.process(&models.Update{Message: msg})
	calls := tg.snapshot()
	if len(calls) != 1 || !strings.HasPrefix(calls[0].text, "Content saved as PRIVATE with [memos/new]") {
		t.Fatalf("unexpected replies: %+v", calls)
	}
}
