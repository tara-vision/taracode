package assistant

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ImageData represents a base64-encoded image with its MIME type
type ImageData struct {
	Path     string // Original file path (for display)
	MimeType string // MIME type (e.g., "image/png")
	Base64   string // Base64-encoded image data
}

// Supported image extensions and their MIME types
var imageExtensions = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".gif":  "image/gif",
	".webp": "image/webp",
	".bmp":  "image/bmp",
}

// IsImageFile checks if a file path refers to an image based on extension
func IsImageFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	_, ok := imageExtensions[ext]
	return ok
}

// GetImageMimeType returns the MIME type for an image file
func GetImageMimeType(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mime, ok := imageExtensions[ext]; ok {
		return mime
	}
	return "application/octet-stream"
}

// LoadImage reads an image file and returns ImageData with base64 encoding
func LoadImage(path string) (*ImageData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read image %s: %w", path, err)
	}

	// Check file size (limit to 20MB for safety)
	if len(data) > 20*1024*1024 {
		return nil, fmt.Errorf("image %s is too large (max 20MB)", path)
	}

	return &ImageData{
		Path:     path,
		MimeType: GetImageMimeType(path),
		Base64:   base64.StdEncoding.EncodeToString(data),
	}, nil
}

// ToDataURL converts ImageData to a data URL suitable for OpenAI API
func (i *ImageData) ToDataURL() string {
	return fmt.Sprintf("data:%s;base64,%s", i.MimeType, i.Base64)
}
