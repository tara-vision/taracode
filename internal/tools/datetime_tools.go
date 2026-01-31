package tools

import (
	"fmt"
	"time"
)

// GetDateTime returns the current date and time in multiple formats
func GetDateTime(params map[string]interface{}, workingDir string) (string, error) {
	now := time.Now()

	// Get optional timezone parameter
	tzName := "Local"
	if tz, ok := params["timezone"].(string); ok && tz != "" {
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return "", fmt.Errorf("invalid timezone %q: %w", tz, err)
		}
		now = now.In(loc)
		tzName = tz
	}

	// Get optional format parameter
	format := "full"
	if f, ok := params["format"].(string); ok && f != "" {
		format = f
	}

	var result string
	switch format {
	case "date":
		result = now.Format("Monday, January 2, 2006")
	case "time":
		result = now.Format("3:04:05 PM MST")
	case "iso":
		result = now.Format(time.RFC3339)
	case "unix":
		result = fmt.Sprintf("%d", now.Unix())
	case "full":
		fallthrough
	default:
		result = fmt.Sprintf("Current date and time (%s):\n", tzName)
		result += fmt.Sprintf("  Date: %s\n", now.Format("Monday, January 2, 2006"))
		result += fmt.Sprintf("  Time: %s\n", now.Format("3:04:05 PM MST"))
		result += fmt.Sprintf("  ISO:  %s\n", now.Format(time.RFC3339))
		result += fmt.Sprintf("  Unix: %d", now.Unix())
	}

	return result, nil
}
