package watch

import (
	"testing"
)

func TestHammingDistance(t *testing.T) {
	tests := []struct {
		name     string
		hash1    uint64
		hash2    uint64
		expected int
	}{
		{"identical", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0},
		{"completely different", 0x0000000000000000, 0xFFFFFFFFFFFFFFFF, 64},
		{"single bit", 0x0000000000000001, 0x0000000000000000, 1},
		{"half different", 0x00000000FFFFFFFF, 0xFFFFFFFF00000000, 64},
		{"quarter different", 0x00000000FFFF0000, 0x00000000FFFFFFFF, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HammingDistance(tt.hash1, tt.hash2)
			if result != tt.expected {
				t.Errorf("HammingDistance(%x, %x) = %d, want %d", tt.hash1, tt.hash2, result, tt.expected)
			}
		})
	}
}

func TestHashDifferenceRatio(t *testing.T) {
	tests := []struct {
		name     string
		hash1    uint64
		hash2    uint64
		expected float64
	}{
		{"identical", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0.0},
		{"completely different", 0x0000000000000000, 0xFFFFFFFFFFFFFFFF, 1.0},
		{"50% different", 0x00000000FFFFFFFF, 0xFFFFFFFF00000000, 1.0}, // all 64 bits differ
		{"25% different", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFF00000000, 0.5}, // 32 bits differ
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HashDifferenceRatio(tt.hash1, tt.hash2)
			if result != tt.expected {
				t.Errorf("HashDifferenceRatio(%x, %x) = %f, want %f", tt.hash1, tt.hash2, result, tt.expected)
			}
		})
	}
}

func TestHasSignificantChange(t *testing.T) {
	tests := []struct {
		name      string
		oldHash   uint64
		newHash   uint64
		threshold float64
		expected  bool
	}{
		{"identical below threshold", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFF, 0.15, false},
		{"5% change below 15% threshold", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFFFFFFC, 0.15, false},     // 2 bits = 3.1%
		{"20% change above 15% threshold", 0xFFFFFFFFFFFFFFFF, 0xFFFFFFFFFFF00000, 0.15, true},      // 20 bits = 31%
		{"completely different", 0x0000000000000000, 0xFFFFFFFFFFFFFFFF, 0.15, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasSignificantChange(tt.oldHash, tt.newHash, tt.threshold)
			if result != tt.expected {
				ratio := HashDifferenceRatio(tt.oldHash, tt.newHash)
				t.Errorf("HasSignificantChange(%x, %x, %.2f) = %v, want %v (ratio: %.2f)",
					tt.oldHash, tt.newHash, tt.threshold, result, tt.expected, ratio)
			}
		})
	}
}

func TestQuickDiff(t *testing.T) {
	// Test with non-existent files
	result := QuickDiff("/nonexistent/file1.png", "/nonexistent/file2.png")
	if !result {
		t.Error("QuickDiff should return true for non-existent files")
	}
}
