package bot

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/go-telegram/bot/models"
)

// messageContent returns the memo text for a message: its text or caption,
// with supported formatting converted to Markdown, after a line naming the
// original sender if it was forwarded.
func messageContent(msg *models.Message) string {
	content := formattedContent(msg)
	if msg.ForwardOrigin != nil {
		content = forwardHeader(msg.ForwardOrigin) + "\n" + content
	}
	return content
}

// formattedContent returns a message's text or caption with supported
// formatting converted to Markdown, without the forwarded-sender header, so
// that edits reuse the same conversion as new memos.
func formattedContent(msg *models.Message) string {
	content, entities := msg.Text, msg.Entities
	if msg.Caption != "" {
		content, entities = msg.Caption, msg.CaptionEntities
	}
	if len(entities) > 0 {
		content = formatContent(content, entities)
	}
	return content
}

// forwardHeader names the original sender of a forwarded message, linking to
// it when it has a public username.
func forwardHeader(origin *models.MessageOrigin) string {
	var name, username string
	switch {
	case origin.MessageOriginUser != nil:
		user := origin.MessageOriginUser.SenderUser
		name = strings.TrimSpace(user.FirstName + " " + user.LastName)
		username = user.Username
	case origin.MessageOriginHiddenUser != nil:
		name = origin.MessageOriginHiddenUser.SenderUserName
		if name == "" {
			name = "Hidden User"
		}
	case origin.MessageOriginChat != nil:
		name, username = origin.MessageOriginChat.SenderChat.Title, origin.MessageOriginChat.SenderChat.Username
	case origin.MessageOriginChannel != nil:
		name, username = origin.MessageOriginChannel.Chat.Title, origin.MessageOriginChannel.Chat.Username
	}
	if name == "" {
		name = "an unknown sender"
	}
	if username != "" {
		return fmt.Sprintf("Forwarded from [%s](https://t.me/%s)", name, username)
	}
	return "Forwarded from " + name
}

// formatContent converts Telegram entities to Markdown. Entity offsets count
// UTF-16 code units. Entities that overlap an earlier one are skipped, so
// nested formatting keeps only the outermost style.
func formatContent(content string, entities []models.MessageEntity) string {
	entities = slices.Clone(entities)
	slices.SortStableFunc(entities, func(a, b models.MessageEntity) int {
		return cmp.Or(cmp.Compare(a.Offset, b.Offset), cmp.Compare(a.Length, b.Length))
	})

	units := utf16.Encode([]rune(content))
	var sb strings.Builder
	cursor := 0
	for _, entity := range entities {
		if !isSupportedEntity(entity.Type) {
			continue
		}
		start, end := entity.Offset, min(entity.Offset+entity.Length, len(units))
		if start < cursor {
			continue
		}
		if start >= len(units) {
			break
		}
		sb.WriteString(string(utf16.Decode(units[cursor:start])))
		sb.WriteString(formatSegment(string(utf16.Decode(units[start:end])), entity))
		cursor = end
	}
	sb.WriteString(string(utf16.Decode(units[cursor:])))
	return sb.String()
}

func isSupportedEntity(entityType models.MessageEntityType) bool {
	switch entityType {
	case models.MessageEntityTypeURL,
		models.MessageEntityTypeTextLink,
		models.MessageEntityTypeTextMention,
		models.MessageEntityTypeBold,
		models.MessageEntityTypeItalic,
		models.MessageEntityTypeStrikethrough,
		models.MessageEntityTypeCode,
		models.MessageEntityTypePre,
		models.MessageEntityTypeBlockquote,
		models.MessageEntityTypeExpandableBlockquote:
		return true
	default:
		// Underline and spoiler have no Markdown spelling, so they stay plain
		// text rather than turning into markup Memos would not render.
		return false
	}
}

// formatSegment formats an entity's text. Code blocks and quotes keep their
// line structure, so they are formatted as a whole; every other entity is
// formatted per line, because Markdown markers cannot span a blank line and
// whitespace must stay outside them.
func formatSegment(segment string, entity models.MessageEntity) string {
	switch entity.Type {
	case models.MessageEntityTypePre:
		return codeBlock(segment, entity.Language)
	case models.MessageEntityTypeCode:
		return inlineCode(segment)
	case models.MessageEntityTypeBlockquote, models.MessageEntityTypeExpandableBlockquote:
		return blockquote(segment)
	}
	lines := strings.Split(segment, "\n")
	for i, line := range lines {
		lines[i] = formatLine(line, entity)
	}
	return strings.Join(lines, "\n")
}

// codeBlock renders a Telegram pre entity as a fenced code block. Telegram
// leaves Language empty for unlabelled blocks, which then get a bare fence.
func codeBlock(segment, language string) string {
	text := strings.TrimSuffix(segment, "\n")
	language = strings.TrimSpace(language)
	if strings.Contains(text, "```") {
		// A fence inside the block would close it early: use the next longer
		// fence, which Markdown still reads as a single code block.
		return "````\n" + text + "\n````"
	}
	return "```" + language + "\n" + text + "\n```"
}

// inlineCode renders a Telegram code entity as a Markdown code span.
func inlineCode(segment string) string {
	if strings.Contains(segment, "`") {
		return "``" + segment + "``"
	}
	return "`" + segment + "`"
}

// blockquote prefixes every line, including blank ones, so the quoted block
// keeps its shape.
func blockquote(segment string) string {
	lines := strings.Split(strings.TrimSuffix(segment, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

func formatLine(line string, entity models.MessageEntity) string {
	core := strings.TrimSpace(line)
	if core == "" {
		return line
	}
	start := strings.Index(line, core)
	prefix, suffix := line[:start], line[start+len(core):]
	switch entity.Type {
	case models.MessageEntityTypeURL:
		core = fmt.Sprintf("[%s](%s)", core, core)
	case models.MessageEntityTypeTextLink:
		core = fmt.Sprintf("[%s](%s)", core, entity.URL)
	case models.MessageEntityTypeTextMention:
		if entity.User != nil {
			core = fmt.Sprintf("[%s](tg://user?id=%d)", core, entity.User.ID)
		}
	case models.MessageEntityTypeBold:
		core = "**" + core + "**"
	case models.MessageEntityTypeItalic:
		core = "*" + core + "*"
	case models.MessageEntityTypeStrikethrough:
		core = "~~" + core + "~~"
	}
	return prefix + core + suffix
}
