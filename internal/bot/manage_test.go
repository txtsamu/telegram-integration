package bot

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-telegram/bot/models"

	"github.com/usememos/memogram/internal/memosapi"
)

// deleteService builds a Service whose Memos has memo memos/abc, and fails the
// test on any request the routes do not cover.
func deleteService(t *testing.T, routes map[string]http.HandlerFunc) (*Service, *fakeTelegram) {
	t.Helper()
	tg := newFakeTelegram(t, nil)
	data := openStore(t)
	bindUser(t, data, 1, "token")
	all := map[string]http.HandlerFunc{
		"GET /api/memos/abc": respond(`{"name":"memos/abc","content":"note","visibility":"PRIVATE","state":"NORMAL"}`),
	}
	for route, handler := range routes {
		all[route] = handler
	}
	return newTestService(t, tg, Options{Memos: fakeMemos(t, all), Store: data}), tg
}

func TestDeleteButtonAsksFirst(t *testing.T) {
	s, tg := deleteService(t, nil)
	calls := press(s, tg, "delete memos/abc")
	if methods(calls) != "editMessageText,answerCallbackQuery" || calls[1].text != "Confirm the deletion" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	want := fmt.Sprintf("Delete [memos/abc](%s/memos/abc)?\nThis cannot be undone\\.", s.memos.BaseURL())
	if calls[0].text != want {
		t.Fatalf("confirmation %q, want %q", calls[0].text, want)
	}
	if got, want := keyboardLayout(&calls[0].keyboard), "Yes, delete=del! memos/abc,Cancel=back memos/abc"; got != want {
		t.Fatalf("confirmation buttons %q, want %q", got, want)
	}
}

func TestDeleteConfirmationDeletesMemo(t *testing.T) {
	var deletes atomic.Int32
	var path string
	s, tg := deleteService(t, map[string]http.HandlerFunc{
		"DELETE /api/memos/abc": func(w http.ResponseWriter, r *http.Request) {
			deletes.Add(1)
			path = r.URL.Path
			fmt.Fprint(w, `{"name":"memos/abc"}`)
		},
	})
	calls := press(s, tg, "del! memos/abc")
	if deletes.Load() != 1 || path != "/api/memos/abc" {
		t.Fatalf("delete calls=%d path=%q", deletes.Load(), path)
	}
	if methods(calls) != "editMessageText,answerCallbackQuery" || calls[1].text != "Memo deleted" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if want := fmt.Sprintf("Memo [memos/abc](%s/memos/abc) deleted", s.memos.BaseURL()); calls[0].text != want {
		t.Fatalf("confirmation %q, want %q", calls[0].text, want)
	}
	if got := keyboardLayout(&calls[0].keyboard); got != "" {
		t.Fatalf("deleted memo kept buttons: %q", got)
	}
}

func TestDeleteCancelRestoresMemo(t *testing.T) {
	s, tg := deleteService(t, nil)
	calls := press(s, tg, "back memos/abc")
	if methods(calls) != "editMessageText,answerCallbackQuery" || calls[1].text != "Memo unchanged" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	want := fmt.Sprintf("Memo unchanged as PRIVATE with [memos/abc](%s/memos/abc)", s.memos.BaseURL())
	if calls[0].text != want {
		t.Fatalf("restored text %q, want %q", calls[0].text, want)
	}
	if !strings.Contains(keyboardLayout(&calls[0].keyboard), "Delete=delete memos/abc") {
		t.Fatalf("standard buttons were not restored: %s", keyboardLayout(&calls[0].keyboard))
	}
}

func TestDeleteConfirmationFailureIsVisible(t *testing.T) {
	// A swallowed edit error is what made Delete look like a no-op, so the user
	// must be told when the confirmation cannot be shown.
	s, tg := deleteService(t, nil)
	tg.failEdits = true
	calls := press(s, tg, "delete memos/abc")
	if len(calls) != 1 || calls[0].method != "answerCallbackQuery" || !calls[0].alert ||
		calls[0].text != "Could not show the confirmation, try again" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestDeleteFailureIsReported(t *testing.T) {
	s, tg := deleteService(t, map[string]http.HandlerFunc{
		"DELETE /api/memos/abc": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"storage unavailable"}`)
		},
	})
	calls := press(s, tg, "del! memos/abc")
	if len(calls) != 1 || calls[0].method != "answerCallbackQuery" || calls[0].text != "Failed to delete memo" || !calls[0].alert {
		t.Fatalf("unexpected calls: %+v", calls)
	}
}

func TestDeleteReplyCommand(t *testing.T) {
	var deletes atomic.Int32
	s, tg := deleteService(t, map[string]http.HandlerFunc{
		"DELETE /api/memos/abc": func(w http.ResponseWriter, _ *http.Request) {
			deletes.Add(1)
			fmt.Fprint(w, `{}`)
		},
	})
	calls := send(s, tg, &models.Message{
		ID: 5, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}, From: &models.User{ID: 1},
		Text:           "/delete",
		ReplyToMessage: botMessage(savedMemoReply),
	})
	if deletes.Load() != 1 {
		t.Fatalf("delete was not called: %d", deletes.Load())
	}
	if len(calls) != 1 || calls[0].method != "sendMessage" {
		t.Fatalf("unexpected calls: %+v", calls)
	}
	if want := fmt.Sprintf("Memo [memos/abc](%s/memos/abc) deleted", s.memos.BaseURL()); calls[0].text != want {
		t.Fatalf("confirmation %q, want %q", calls[0].text, want)
	}
}

func TestDeleteReplyCommandIsCaseInsensitive(t *testing.T) {
	for _, text := range []string{"/del", "/DELETE", " /Delete "} {
		t.Run(text, func(t *testing.T) {
			msg := &models.Message{Text: text}
			if !isDeleteReply(msg) {
				t.Fatalf("%q was not recognised", text)
			}
		})
	}
	for _, text := range []string{"/delete now", "delete", "/deleted"} {
		msg := &models.Message{Text: text}
		if isDeleteReply(msg) {
			t.Fatalf("%q should not delete a memo", text)
		}
	}
}

func TestMarkdownDocumentBecomesMemoContent(t *testing.T) {
	doc := []byte("# Heading\n\nSome **text** and a list:\n\n- one\n- two\n")
	tg := newFakeTelegram(t, map[string]fakeFile{
		"file": {path: "documents/file_7.md", contentType: "text/markdown", content: doc},
	})
	var created memosapi.Memo
	var uploads atomic.Int32
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"POST /api/memos": func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"name":"memos/md","visibility":"PRIVATE","state":"NORMAL"}`)
			},
			"POST /api/attachments": func(w http.ResponseWriter, _ *http.Request) {
				uploads.Add(1)
				fmt.Fprint(w, `{"name":"attachments/x"}`)
			},
		}),
		Store: data,
	})
	calls := send(s, tg, &models.Message{
		ID: 1, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}, From: &models.User{ID: 1},
		Document: &models.Document{FileID: "file", FileName: "notes.md", MimeType: "text/markdown"},
	})
	if got, want := created.Content, strings.TrimSpace(string(doc)); got != want {
		t.Fatalf("memo content %q, want %q", got, want)
	}
	if uploads.Load() != 0 {
		t.Fatalf("markdown document was also uploaded as an attachment")
	}
	if len(calls) != 1 || !strings.HasPrefix(calls[0].text, "Content saved as PRIVATE") {
		t.Fatalf("unexpected replies: %+v", calls)
	}
}

func TestMarkdownDocumentKeepsCaption(t *testing.T) {
	tg := newFakeTelegram(t, map[string]fakeFile{
		"file": {path: "documents/file_7.md", contentType: "text/markdown", content: []byte("# Body\n")},
	})
	var created memosapi.Memo
	data := openStore(t)
	bindUser(t, data, 1, "token")
	s := newTestService(t, tg, Options{
		Memos: fakeMemos(t, map[string]http.HandlerFunc{
			"POST /api/memos": func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
					t.Error(err)
				}
				fmt.Fprint(w, `{"name":"memos/md","visibility":"PRIVATE","state":"NORMAL"}`)
			},
		}),
		Store: data,
	})
	send(s, tg, &models.Message{
		ID: 1, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}, From: &models.User{ID: 1},
		Caption:  "caption first",
		Document: &models.Document{FileID: "file", FileName: "notes.md", MimeType: "text/markdown"},
	})
	if want := "caption first\n\n# Body"; created.Content != want {
		t.Fatalf("memo content %q, want %q", created.Content, want)
	}
}

func TestNonMarkdownDocumentIsAttached(t *testing.T) {
	for name, file := range map[string]struct {
		name, mime string
	}{
		"text file":       {name: "notes.txt", mime: "text/plain"},
		"binary document": {name: "report.pdf", mime: "application/pdf"},
	} {
		t.Run(name, func(t *testing.T) {
			tg := newFakeTelegram(t, map[string]fakeFile{
				"file": {path: "documents/file_9", contentType: file.mime, content: []byte("payload")},
			})
			var uploads atomic.Int32
			data := openStore(t)
			bindUser(t, data, 1, "token")
			s := newTestService(t, tg, Options{
				Memos: fakeMemos(t, map[string]http.HandlerFunc{
					"POST /api/memos": respond(`{"name":"memos/f","visibility":"PRIVATE","state":"NORMAL"}`),
					"POST /api/attachments": func(w http.ResponseWriter, _ *http.Request) {
						uploads.Add(1)
						fmt.Fprint(w, `{"name":"attachments/x"}`)
					},
				}),
				Store: data,
			})
			send(s, tg, &models.Message{
				ID: 1, Chat: models.Chat{ID: 1, Type: models.ChatTypePrivate}, From: &models.User{ID: 1},
				Document: &models.Document{FileID: "file", FileName: file.name, MimeType: file.mime},
			})
			if uploads.Load() != 1 {
				t.Fatalf("%s was not attached", file.name)
			}
		})
	}
}
