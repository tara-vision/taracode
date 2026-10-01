package cmd

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain renders lipgloss styles without colour, so the tests compare plain text whether or not
// the terminal or CLICOLOR_FORCE asks for colour.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}
