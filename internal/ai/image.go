package ai

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// imageExts is the set of file extensions the vision tools accept, mapped to
// the IANA media type used when the extension is trustworthy. The real media
// type is confirmed from the file's content in EncodeImageFile, so this map is
// only a fast gate for routing (see IsImagePath).
var imageExts = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// supportedImageTypes is the set of media types the vision providers accept.
var supportedImageTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

// IsImagePath reports whether path has a supported image extension. It is a
// cheap routing check (no file read); actual validation happens in
// EncodeImageFile.
func IsImagePath(path string) bool {
	_, ok := imageExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

// SupportedImageExts lists the accepted extensions for use in help text.
func SupportedImageExts() string { return "png, jpg, jpeg, gif, webp" }

// EncodeImageFile reads the image at path and returns it as base64 ImageContent.
// The media type is detected from the file's content (not just its extension),
// so a mislabelled or corrupt file is rejected with an error rather than sent to
// the model. Errors are returned for unreadable files, empty files, and content
// that is not one of the supported image formats.
func EncodeImageFile(path string) (ImageContent, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ImageContent{}, err
	}
	if len(data) == 0 {
		return ImageContent{}, fmt.Errorf("image file is empty: %s", path)
	}
	// http.DetectContentType sniffs the leading bytes; it returns an image/* type
	// for real images and something else (e.g. application/octet-stream) for
	// corrupt or non-image data.
	mediaType := http.DetectContentType(data)
	// DetectContentType may append parameters (e.g. "; charset=..."); keep the
	// bare type for the media_type field.
	if i := strings.IndexByte(mediaType, ';'); i >= 0 {
		mediaType = strings.TrimSpace(mediaType[:i])
	}
	if !supportedImageTypes[mediaType] {
		return ImageContent{}, fmt.Errorf("%s is not a supported image (detected %q; supported: %s)", path, mediaType, SupportedImageExts())
	}
	return ImageContent{
		MediaType: mediaType,
		Data:      base64.StdEncoding.EncodeToString(data),
	}, nil
}

// DataURI returns the image as an RFC 2397 data URI, the shape OpenAI-compatible
// providers expect in an image_url content part.
func (img ImageContent) DataURI() string {
	return "data:" + img.MediaType + ";base64," + img.Data
}
