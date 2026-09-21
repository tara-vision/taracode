package cmd

import (
	"fmt"
	"strings"

	"github.com/tara-vision/taracode/internal/assistant"
	"github.com/tara-vision/taracode/internal/llm"
)

// thinkValues is what /think accepts, in the order shown to the user.
var thinkValues = []string{"auto", "off", "on", "low", "medium", "high"}

// handleThink shows or changes the reasoning mode used for later requests.
func handleThink(asst *assistant.Assistant, args []string) {
	if len(args) == 0 {
		fmt.Printf("Reasoning mode: %s\n", displayThink(asst.Think()))
		fmt.Println()
		return
	}

	think, ok := llm.ParseThink(args[0])
	if !ok {
		fmt.Printf("Unknown reasoning mode %q. Use one of: %s\n", args[0], strings.Join(thinkValues, ", "))
		fmt.Println()
		return
	}

	asst.SetThink(think)
	fmt.Printf("Reasoning mode set to %s.\n", displayThink(think))
	fmt.Println()
}

// displayThink shows ThinkAuto (stored as "") as "auto" instead of an empty string.
func displayThink(t llm.Think) string {
	if t == llm.ThinkAuto {
		return "auto"
	}
	return string(t)
}
