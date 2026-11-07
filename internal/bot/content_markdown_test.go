package bot

import (
	"testing"

	"github.com/go-telegram/bot/models"
)

// formatOne formats a segment the way formatContent would for a single entity.
func formatOne(content string, entity models.MessageEntity) string {
	return formatContent(content, []models.MessageEntity{entity})
}

func TestFormatContent_CodeBlock(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		entity  models.MessageEntity
		want    string
	}{
		"with language": {
			content: "fmt.Println(\"hi\")",
			entity:  models.MessageEntity{Type: models.MessageEntityTypePre, Length: 17, Language: "go"},
			want:    "```go\nfmt.Println(\"hi\")\n```",
		},
		"without language": {
			content: "plain block",
			entity:  models.MessageEntity{Type: models.MessageEntityTypePre, Length: 11},
			want:    "```\nplain block\n```",
		},
		"multiline keeps its newlines": {
			content: "line one\nline two",
			entity:  models.MessageEntity{Type: models.MessageEntityTypePre, Length: 17, Language: "sh"},
			want:    "```sh\nline one\nline two\n```",
		},
		"fence inside the block lengthens the fence": {
			content: "outer\n```\ninner",
			entity:  models.MessageEntity{Type: models.MessageEntityTypePre, Length: 15},
			want:    "````\nouter\n```\ninner\n````",
		},
		"surrounding text is untouched": {
			content: "see below\ncode\nthanks",
			entity:  models.MessageEntity{Type: models.MessageEntityTypePre, Offset: 10, Length: 4, Language: "sh"},
			want:    "see below\n```sh\ncode\n```\nthanks",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := formatOne(tc.content, tc.entity); got != tc.want {
				t.Fatalf("want %q, got %q", tc.want, got)
			}
		})
	}
}

func TestFormatContent_InlineCode(t *testing.T) {
	if got, want := formatOne("memos --version", models.MessageEntity{Type: models.MessageEntityTypeCode, Length: 15}), "`memos --version`"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	if got, want := formatOne("a`b", models.MessageEntity{Type: models.MessageEntityTypeCode, Length: 3}), "``a`b``"; got != want {
		t.Fatalf("backtick inside: want %q, got %q", want, got)
	}
}

func TestFormatContent_Blockquote(t *testing.T) {
	for name, entityType := range map[string]models.MessageEntityType{
		"blockquote":            models.MessageEntityTypeBlockquote,
		"expandable blockquote": models.MessageEntityTypeExpandableBlockquote,
	} {
		t.Run(name, func(t *testing.T) {
			content := "first line\nsecond line"
			want := "> first line\n> second line"
			if got := formatOne(content, models.MessageEntity{Type: entityType, Length: 23}); got != want {
				t.Fatalf("want %q, got %q", want, got)
			}
		})
	}
}

func TestFormatContent_Strikethrough(t *testing.T) {
	if got, want := formatOne("done", models.MessageEntity{Type: models.MessageEntityTypeStrikethrough, Length: 4}), "~~done~~"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestFormatContent_TextMention(t *testing.T) {
	entity := models.MessageEntity{Type: models.MessageEntityTypeTextMention, Length: 5, User: &models.User{ID: 42}}
	if got, want := formatOne("Alice", entity), "[Alice](tg://user?id=42)"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
	// Without a user there is nothing to link to, so the text stays as it is.
	if got, want := formatOne("Alice", models.MessageEntity{Type: models.MessageEntityTypeTextMention, Length: 5}), "Alice"; got != want {
		t.Fatalf("want %q, got %q", want, got)
	}
}

func TestFormatContent_UnsupportedEntitiesStayPlain(t *testing.T) {
	for name, entityType := range map[string]models.MessageEntityType{
		"underline": models.MessageEntityTypeUnderline,
		"spoiler":   models.MessageEntityTypeSpoiler,
	} {
		t.Run(name, func(t *testing.T) {
			if got, want := formatOne("text", models.MessageEntity{Type: entityType, Length: 4}), "text"; got != want {
				t.Fatalf("want %q, got %q", want, got)
			}
		})
	}
}
