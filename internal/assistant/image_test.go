package assistant

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsImageFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"image.png", true},
		{"image.PNG", true},
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"photo.JPEG", true},
		{"animation.gif", true},
		{"modern.webp", true},
		{"bitmap.bmp", true},
		{"document.txt", false},
		{"script.go", false},
		{"config.yaml", false},
		{"no_extension", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := IsImageFile(tt.path)
			if result != tt.expected {
				t.Errorf("IsImageFile(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestGetImageMimeType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"image.png", "image/png"},
		{"photo.jpg", "image/jpeg"},
		{"photo.jpeg", "image/jpeg"},
		{"animation.gif", "image/gif"},
		{"modern.webp", "image/webp"},
		{"bitmap.bmp", "image/bmp"},
		{"unknown.xyz", "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := GetImageMimeType(tt.path)
			if result != tt.expected {
				t.Errorf("GetImageMimeType(%q) = %v, expected %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestLoadImage(t *testing.T) {
	// Create a temporary test image (1x1 PNG)
	tmpDir := t.TempDir()
	testImage := filepath.Join(tmpDir, "test.png")

	// Minimal valid PNG (1x1 transparent pixel)
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4,
		0x89, 0x00, 0x00, 0x00, 0x0A, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, 0xAE, // IEND chunk
		0x42, 0x60, 0x82,
	}

	if err := os.WriteFile(testImage, pngData, 0644); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}

	t.Run("valid image", func(t *testing.T) {
		img, err := LoadImage(testImage)
		if err != nil {
			t.Fatalf("LoadImage failed: %v", err)
		}

		if img.MimeType != "image/png" {
			t.Errorf("MimeType = %v, expected image/png", img.MimeType)
		}

		if img.Base64 == "" {
			t.Error("Base64 is empty")
		}

		if img.Path != testImage {
			t.Errorf("Path = %v, expected %v", img.Path, testImage)
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		_, err := LoadImage(filepath.Join(tmpDir, "nonexistent.png"))
		if err == nil {
			t.Error("Expected error for non-existent file")
		}
	})
}

func TestImageDataToDataURL(t *testing.T) {
	img := &ImageData{
		Path:     "test.png",
		MimeType: "image/png",
		Base64:   "dGVzdA==", // "test" in base64
	}

	expected := "data:image/png;base64,dGVzdA=="
	result := img.ToDataURL()

	if result != expected {
		t.Errorf("ToDataURL() = %v, expected %v", result, expected)
	}
}
