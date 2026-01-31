package watch

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
)

// Simple perceptual hash implementation using average hash algorithm
// This avoids external dependencies while providing reasonable change detection

// ComputeHash generates a perceptual hash for an image file
// Returns a 64-bit hash where similar images have similar hash values
func ComputeHash(imagePath string) (uint64, error) {
	file, err := os.Open(imagePath)
	if err != nil {
		return 0, fmt.Errorf("failed to open image: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return 0, fmt.Errorf("failed to decode image: %w", err)
	}

	return averageHash(img), nil
}

// averageHash computes an 8x8 average hash of the image
// This is a simple but effective perceptual hash algorithm
func averageHash(img image.Image) uint64 {
	// Resize to 8x8 using simple sampling
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()

	// Sample grid size
	const hashSize = 8
	pixels := make([]uint8, hashSize*hashSize)

	// Sample the image at 8x8 grid points and convert to grayscale
	for y := 0; y < hashSize; y++ {
		for x := 0; x < hashSize; x++ {
			// Map to original image coordinates
			srcX := bounds.Min.X + (x * width / hashSize)
			srcY := bounds.Min.Y + (y * height / hashSize)

			// Get pixel and convert to grayscale
			r, g, b, _ := img.At(srcX, srcY).RGBA()
			// Convert from 16-bit to 8-bit and compute luminance
			gray := uint8((0.299*float64(r>>8) + 0.587*float64(g>>8) + 0.114*float64(b>>8)))
			pixels[y*hashSize+x] = gray
		}
	}

	// Compute average
	var sum uint32
	for _, p := range pixels {
		sum += uint32(p)
	}
	avg := uint8(sum / uint32(len(pixels)))

	// Generate hash: 1 if pixel > average, 0 otherwise
	var hash uint64
	for i, p := range pixels {
		if p > avg {
			hash |= 1 << uint(63-i)
		}
	}

	return hash
}

// HammingDistance returns the number of differing bits between two hashes
func HammingDistance(hash1, hash2 uint64) int {
	diff := hash1 ^ hash2
	count := 0
	for diff != 0 {
		count++
		diff &= diff - 1 // Clear lowest set bit
	}
	return count
}

// HashDifferenceRatio returns the percentage of differing bits (0.0-1.0)
func HashDifferenceRatio(hash1, hash2 uint64) float64 {
	distance := HammingDistance(hash1, hash2)
	return float64(distance) / 64.0 // 64 bits in hash
}

// HasSignificantChange determines if two images differ significantly
// threshold is the minimum difference ratio (0.0-1.0) to consider significant
// Default recommendation: 0.15 (15% difference)
func HasSignificantChange(oldHash, newHash uint64, threshold float64) bool {
	if threshold <= 0 {
		threshold = 0.15 // Default 15%
	}
	if threshold > 1.0 {
		threshold = 1.0
	}

	ratio := HashDifferenceRatio(oldHash, newHash)
	return ratio >= threshold
}

// CompareImages compares two image files and returns if they differ significantly
func CompareImages(path1, path2 string, threshold float64) (bool, error) {
	hash1, err := ComputeHash(path1)
	if err != nil {
		return false, fmt.Errorf("failed to hash first image: %w", err)
	}

	hash2, err := ComputeHash(path2)
	if err != nil {
		return false, fmt.Errorf("failed to hash second image: %w", err)
	}

	return HasSignificantChange(hash1, hash2, threshold), nil
}

// QuickDiff performs a quick file-size based diff before computing hash
// Returns true if files are likely different (different sizes or missing)
func QuickDiff(path1, path2 string) bool {
	info1, err1 := os.Stat(path1)
	info2, err2 := os.Stat(path2)

	// If either file doesn't exist, they're different
	if err1 != nil || err2 != nil {
		return true
	}

	// If sizes differ significantly (>10%), likely different
	size1 := info1.Size()
	size2 := info2.Size()

	if size1 == 0 || size2 == 0 {
		return true
	}

	ratio := float64(size1) / float64(size2)
	if ratio < 0.9 || ratio > 1.1 {
		return true
	}

	return false
}
