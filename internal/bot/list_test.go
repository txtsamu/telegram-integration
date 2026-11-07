package bot

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memosapi"
)

// memosapiMemo builds the part of a memo that a preview reads.
type memosapiMemo struct{ content, title string }

func (m memosapiMemo) toMemo() memosapi.Memo {
	return memosapi.Memo{Content: m.content, Property: memosapi.MemoProperty{Title: m.title}}
}

// linkEntity matches a rendered MarkdownV2 link, whose text and URL are not
// escaped by the bot.
var linkEntity = regexp.MustCompile(`\[[^\[\]]*\]\([^()]*\)`)

// markdownV2Reserved matches every character Telegram's MarkdownV2 parser
// rejects unless it carries a preceding backslash.
var markdownV2Reserved = regexp.MustCompile("[_*\\[\\]()~`>#+\\-=|{}.!]")

// assertMarkdownV2 fails when text would be rejected by Telegram with "can't
// parse entities". Links are removed first, then escaped pairs, and whatever
// reserved character is left is unescaped.
func assertMarkdownV2(t *testing.T, where, text string) {
	t.Helper()
	stripped := linkEntity.ReplaceAllString(text, "")
	var leftover strings.Builder
	for i := 0; i < len(stripped); i++ {
		if stripped[i] == '\\' && i+1 < len(stripped) {
			i++
			continue
		}
		leftover.WriteByte(stripped[i])
	}
	if bad := markdownV2Reserved.FindString(leftover.String()); bad != "" {
		t.Fatalf("%s sends %q, which MarkdownV2 rejects on %q", where, text, bad)
	}
}

func TestEscapeMarkdownCoversMarkdownV2(t *testing.T) {
	for _, char := range []string{"_", "*", "`", "[", "]", "(", ")", "~", ">", "#", "+", "-", "=", "|", "{", "}", ".", "!"} {
		if got, want := escapeMarkdown(char), `\`+char; got != want {
			t.Fatalf("escaped %q to %q, want %q", char, got, want)
		}
	}
	if got, want := escapeMarkdown(`a\b`), `a\\b`; got != want {
		t.Fatalf("backslash: got %q, want %q", got, want)
	}
}

func TestDeleteConfirmationIsMarkdownV2Safe(t *testing.T) {
	s, tg := deleteService(t, nil)
	calls := press(s, tg, "delete memos/abc")
	assertMarkdownV2(t, "delete confirmation", calls[0].text)
	want := fmt.Sprintf("Delete [memos/abc](%s/memos/abc)?\nThis cannot be undone\\.", s.memos.BaseURL())
	if calls[0].text != want {
		t.Fatalf("confirmation %q, want %q", calls[0].text, want)
	}
}

// listMemos serves the account lookup and a fixed set of memos for /list.
func listMemos(t *testing.T, memos string) map[string]http.HandlerFunc {
	t.Helper()
	return map[string]http.HandlerFunc{
		"GET /api/auth/me": respond(`{"user":{"name":"users/moo","username":"moo"}}`),
		"GET /api/memos":   respond(memos),
	}
}

// listMessage sends the /list command, which is what the handler matches.
func listMessage() *models.Message {
	return textMessage(1, "/list")
}

func TestListSendsOneMessagePerMemo(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{Store: data, Memos: fakeMemos(t, listMemos(t,
		`{"memos":[
			{"name":"memos/aaa","content":"First memo","visibility":"PUBLIC","state":"NORMAL","property":{"title":"First memo"}},
			{"name":"memos/bbb","content":"Second. Memo!","visibility":"PRIVATE","state":"NORMAL"}
		]}`))})
	calls := send(s, tg, listMessage())
	if len(calls) != 2 {
		t.Fatalf("expected one message per memo, got %+v", calls)
	}
	for _, call := range calls {
		if call.method != "sendMessage" {
			t.Fatalf("unexpected method %s", call.method)
		}
		assertMarkdownV2(t, "memo card", call.text)
		if !strings.Contains(keyboardLayout(&call.keyboard), "Delete=delete memos/") {
			t.Fatalf("memo card has no Delete button: %s", keyboardLayout(&call.keyboard))
		}
	}
	if want := "First memo\n[memos/aaa](" + s.memos.BaseURL() + "/memos/aaa)"; calls[0].text != want {
		t.Fatalf("first card %q, want %q", calls[0].text, want)
	}
	// The preview is escaped, so punctuation in a memo cannot break the message.
	if want := "Second\\. Memo\\!\n[memos/bbb](" + s.memos.BaseURL() + "/memos/bbb)"; calls[1].text != want {
		t.Fatalf("second card %q, want %q", calls[1].text, want)
	}
}

func TestListReportsEmptyAccount(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{Store: data, Memos: fakeMemos(t, listMemos(t, `{"memos":[]}`))})
	calls := send(s, tg, listMessage())
	if len(calls) != 1 || calls[0].text != "You have no memos yet. Send me a message to save one." {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestListReportsFailure(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{Store: data, Memos: fakeMemos(t, map[string]http.HandlerFunc{
		"GET /api/auth/me": respond(`{"user":{"name":"users/moo"}}`),
		"GET /api/memos":   func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
	})})
	calls := send(s, tg, listMessage())
	if len(calls) != 1 || calls[0].text != "Failed to list memos" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestListRequiresStart(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	s := newTestService(t, tg, Options{Store: openStore(t)})
	calls := send(s, tg, listMessage())
	if len(calls) != 1 || calls[0].text != startFirst {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestMemoPreviewCutsToOneLine(t *testing.T) {
	for name, tc := range map[string]struct {
		memo memosapiMemo
		want string
	}{
		"first line only": {memo: memosapiMemo{content: "title line\nsecond line"}, want: "title line"},
		"trimmed":         {memo: memosapiMemo{content: "  spaced  \n"}, want: "spaced"},
		"title fallback":  {memo: memosapiMemo{title: "from property"}, want: "from property"},
		"empty":           {memo: memosapiMemo{}, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := memoPreview(tc.memo.toMemo()); got != tc.want {
				t.Fatalf("preview %q, want %q", got, tc.want)
			}
		})
	}
	long := strings.Repeat("a", 200)
	if got := memoPreview(memosapiMemo{content: long}.toMemo()); len([]rune(got)) != memoPreviewLimit {
		t.Fatalf("long preview keeps %d runes, want %d", len([]rune(got)), memoPreviewLimit)
	}
}

func TestSearchTruncatesLongMemos(t *testing.T) {
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	long := strings.Repeat("x", maxTelegramText*2)
	body := fmt.Sprintf(`{"memos":[{"name":"memos/big","content":%q,"visibility":"PRIVATE","state":"NORMAL"}]}`, long)
	s := newTestService(t, tg, Options{Store: data, Memos: fakeMemos(t, listMemos(t, body))})
	calls := send(s, tg, textMessage(1, "/search x"))
	if len(calls) != 1 {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if got := len([]rune(calls[0].text)); got > maxTelegramText {
		t.Fatalf("reply is %d runes, Telegram rejects more than %d", got, maxTelegramText)
	}
}
