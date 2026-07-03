package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"spaish/internal/ai"
)

// ReadImage reads an image file and attaches it for a vision-capable model to
// view. Unlike read_file (which returns text), the image bytes are carried out
// of band via the ImageProducer interface into the tool result's Images, so the
// provider can forward the picture to the model on the next turn.
//
// It is a separate tool rather than an extension of read_file so file ownership
// stays clean and read_file's text contract is unchanged.
type ReadImage struct{}

func (ReadImage) Name() string { return "read_image" }

func (ReadImage) Description() string {
	return "Read an image file (" + ai.SupportedImageExts() + ") and attach it so you can " +
		"see it. Use this for screenshots, diagrams, and other pictures instead of read_file, " +
		"which would return unreadable bytes."
}

func (ReadImage) Schema() map[string]any {
	return objectSchema(map[string]any{
		"path": strProp("Path to the image file to read."),
	}, "path")
}

// imageArgs is the parsed input for a read_image call.
type imageArgs struct {
	Path string `json:"path"`
}

func parseImageArgs(input json.RawMessage) (imageArgs, error) {
	var args imageArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return imageArgs{}, fmt.Errorf("invalid input: %w", err)
	}
	if args.Path == "" {
		return imageArgs{}, fmt.Errorf("path is required")
	}
	return args, nil
}

func (ReadImage) Run(_ context.Context, input json.RawMessage) (string, error) {
	args, err := parseImageArgs(input)
	if err != nil {
		return "", err
	}
	img, err := ai.EncodeImageFile(args.Path)
	if err != nil {
		return "", err
	}
	// The bytes themselves travel via Images (below); the model sees this text as
	// the tool_result caption alongside the attached picture.
	return fmt.Sprintf("attached image %s (%s)", args.Path, img.MediaType), nil
}

// Images implements ImageProducer: it returns the decoded image so the agent can
// attach it to the tool result. Encoding is repeated from Run (both are cheap
// and deterministic) to keep the Tool interface's Run signature unchanged.
func (ReadImage) Images(input json.RawMessage) ([]ai.ImageContent, error) {
	args, err := parseImageArgs(input)
	if err != nil {
		return nil, err
	}
	img, err := ai.EncodeImageFile(args.Path)
	if err != nil {
		return nil, err
	}
	return []ai.ImageContent{img}, nil
}
