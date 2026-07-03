package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writePNG writes a 1x1 PNG to a temp file and returns its path.
func writePNG(t *testing.T) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	path := filepath.Join(t.TempDir(), "pic.png")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write png: %v", err)
	}
	return path
}

func TestEncodeImageFile(t *testing.T) {
	path := writePNG(t)
	img, err := EncodeImageFile(path)
	if err != nil {
		t.Fatalf("EncodeImageFile: %v", err)
	}
	if img.MediaType != "image/png" {
		t.Errorf("MediaType = %q, want image/png", img.MediaType)
	}
	raw, err := base64.StdEncoding.DecodeString(img.Data)
	if err != nil {
		t.Fatalf("Data is not valid base64: %v", err)
	}
	if _, _, err := image.Decode(bytes.NewReader(raw)); err != nil {
		t.Errorf("decoded data is not a valid image: %v", err)
	}
	if uri := img.DataURI(); !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Errorf("DataURI prefix wrong: %q", uri[:min(len(uri), 32)])
	}
}

func TestEncodeImageFileErrors(t *testing.T) {
	dir := t.TempDir()

	// Corrupt / non-image content with an image extension: detected, not sent.
	corrupt := filepath.Join(dir, "fake.png")
	if err := os.WriteFile(corrupt, []byte("this is definitely not a png"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeImageFile(corrupt); err == nil {
		t.Error("expected error for corrupt image, got nil")
	}

	// Empty file.
	empty := filepath.Join(dir, "empty.png")
	if err := os.WriteFile(empty, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := EncodeImageFile(empty); err == nil {
		t.Error("expected error for empty image, got nil")
	}

	// Missing file.
	if _, err := EncodeImageFile(filepath.Join(dir, "nope.png")); err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestIsImagePath(t *testing.T) {
	for _, p := range []string{"a.png", "b.JPG", "c.jpeg", "d.gif", "e.webp"} {
		if !IsImagePath(p) {
			t.Errorf("IsImagePath(%q) = false, want true", p)
		}
	}
	for _, p := range []string{"a.txt", "b.go", "c", "d.pdf"} {
		if IsImagePath(p) {
			t.Errorf("IsImagePath(%q) = true, want false", p)
		}
	}
}

// TestAnthropicImageMessages asserts the constructed SDK params carry image
// content for a user turn and for a tool result, with text-only tool results
// keeping their legacy single-text-block shape (regression).
func TestAnthropicImageMessages(t *testing.T) {
	img := ImageContent{MediaType: "image/png", Data: "QUJD"} // "ABC"

	msgs := []Message{
		{Role: "user", Content: "look at this", Images: []ImageContent{img}},
		{Role: "user", ToolResults: []ToolResult{
			{ToolUseID: "t1", Content: "attached image", Images: []ImageContent{img}},
			{ToolUseID: "t2", Content: "plain text result"}, // regression: no images
		}},
	}
	out := toAnthropicMessages(msgs)
	if len(out) != 2 {
		t.Fatalf("got %d messages, want 2", len(out))
	}

	// First message: text block + image block.
	var sawText, sawImage bool
	for _, b := range out[0].Content {
		if b.OfText != nil {
			sawText = true
		}
		if b.OfImage != nil {
			sawImage = true
			if got := b.OfImage.Source.OfBase64.Data; got != img.Data {
				t.Errorf("image data = %q, want %q", got, img.Data)
			}
			if got := string(b.OfImage.Source.OfBase64.MediaType); got != img.MediaType {
				t.Errorf("image media type = %q, want %q", got, img.MediaType)
			}
		}
	}
	if !sawText || !sawImage {
		t.Errorf("user turn: sawText=%v sawImage=%v, want both true", sawText, sawImage)
	}

	// Second message: two tool results.
	trBlocks := out[1].Content
	if len(trBlocks) != 2 {
		t.Fatalf("got %d tool-result blocks, want 2", len(trBlocks))
	}
	// t1: text + image inside the tool_result content.
	tr1 := trBlocks[0].OfToolResult
	if tr1 == nil {
		t.Fatal("first block is not a tool_result")
	}
	var trText, trImage bool
	for _, c := range tr1.Content {
		if c.OfText != nil {
			trText = true
		}
		if c.OfImage != nil {
			trImage = true
		}
	}
	if !trText || !trImage {
		t.Errorf("tool result with image: text=%v image=%v, want both", trText, trImage)
	}
	// t2 (regression): a single text content block, no images.
	tr2 := trBlocks[1].OfToolResult
	if tr2 == nil {
		t.Fatal("second block is not a tool_result")
	}
	if len(tr2.Content) != 1 || tr2.Content[0].OfText == nil {
		t.Errorf("text-only tool result changed shape: %+v", tr2.Content)
	}
}

// TestAnthropicTextOnlyUnchanged is a regression guard: messages without images
// produce no image blocks.
func TestAnthropicTextOnlyUnchanged(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", ToolResults: []ToolResult{{ToolUseID: "t", Content: "ok"}}},
	}
	for _, m := range toAnthropicMessages(msgs) {
		for _, b := range m.Content {
			if b.OfImage != nil {
				t.Error("unexpected image block in text-only conversation")
			}
		}
	}
}

// TestOpenAIImageMessages checks image_url parts appear for a user turn and that
// tool-result images are forwarded as a following user message.
func TestOpenAIImageMessages(t *testing.T) {
	img := ImageContent{MediaType: "image/png", Data: "QUJD"}

	// User turn with an image → content is a parts array.
	out := toOpenAIMessages("", []Message{{Role: "user", Content: "see", Images: []ImageContent{img}}})
	var userMsg *openAIMsg
	for i := range out {
		if out[i].Role == "user" {
			userMsg = &out[i]
		}
	}
	if userMsg == nil {
		t.Fatal("no user message")
	}
	parts, ok := userMsg.Content.([]openAIContentPart)
	if !ok {
		t.Fatalf("user content is %T, want []openAIContentPart", userMsg.Content)
	}
	var haveImage bool
	for _, p := range parts {
		if p.Type == "image_url" && p.ImageURL != nil && strings.HasPrefix(p.ImageURL.URL, "data:image/png;base64,") {
			haveImage = true
		}
	}
	if !haveImage {
		t.Error("expected an image_url part in user content")
	}

	// Text-only user turn → content stays a plain string (regression).
	out2 := toOpenAIMessages("", []Message{{Role: "user", Content: "hi"}})
	if s, ok := out2[len(out2)-1].Content.(string); !ok || s != "hi" {
		t.Errorf("text-only content = %#v, want string \"hi\"", out2[len(out2)-1].Content)
	}

	// Tool result carrying an image → tool message (string) + trailing user image.
	out3 := toOpenAIMessages("", []Message{{Role: "user", ToolResults: []ToolResult{
		{ToolUseID: "t1", Content: "attached", Images: []ImageContent{img}},
	}}})
	if len(out3) != 2 {
		t.Fatalf("got %d messages, want tool + user image, msgs=%+v", len(out3), out3)
	}
	if out3[0].Role != "tool" {
		t.Errorf("first message role = %q, want tool", out3[0].Role)
	}
	if out3[1].Role != "user" {
		t.Errorf("second message role = %q, want user (forwarded image)", out3[1].Role)
	}
	if _, ok := out3[1].Content.([]openAIContentPart); !ok {
		t.Errorf("forwarded image content is %T, want parts array", out3[1].Content)
	}
	// The tool message content must be JSON-serialisable as a plain string.
	if _, err := json.Marshal(out3[0]); err != nil {
		t.Errorf("tool message not serialisable: %v", err)
	}
}

// TestOllamaImageMessages checks the per-message images field for a user turn
// and the forwarded user message for tool-result images.
func TestOllamaImageMessages(t *testing.T) {
	img := ImageContent{MediaType: "image/png", Data: "QUJD"}

	out := toOllamaMessages("", []Message{{Role: "user", Content: "see", Images: []ImageContent{img}}})
	last := out[len(out)-1]
	if len(last.Images) != 1 || last.Images[0] != img.Data {
		t.Errorf("user images = %v, want [%q]", last.Images, img.Data)
	}

	// Regression: text-only user turn has no images field populated.
	out2 := toOllamaMessages("", []Message{{Role: "user", Content: "hi"}})
	if len(out2[len(out2)-1].Images) != 0 {
		t.Error("text-only message should have no images")
	}

	// Tool result image → tool message + trailing user image message.
	out3 := toOllamaMessages("", []Message{{Role: "user", ToolResults: []ToolResult{
		{ToolUseID: "t1", Content: "attached", Images: []ImageContent{img}},
	}}})
	if len(out3) != 2 || out3[0].Role != "tool" || out3[1].Role != "user" {
		t.Fatalf("unexpected messages: %+v", out3)
	}
	if len(out3[1].Images) != 1 {
		t.Errorf("forwarded user message missing image: %+v", out3[1])
	}
}
