package tools

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tara-vision/taracode/internal/policy"
)

// DateTimeTool returns the current date and time.
func DateTimeTool() *Tool {
	return &Tool{
		Name: "get_datetime", ReadForm: true,
		Description: "Current date and time, optionally in a timezone or as a unix timestamp.",
		Params: []Param{
			{Name: "format", Type: "string", Description: "rfc3339 (default), unix, date or time",
				Enum: []string{"rfc3339", "unix", "date", "time"}},
			{Name: "timezone", Type: "string", Description: "IANA zone such as Europe/Skopje (default: local)"},
		},
		Classify: func(map[string]any, string) policy.Invocation {
			return policy.Invocation{Tool: "get_datetime", Classification: policy.Read}
		},
		Run: func(_ context.Context, args map[string]any, _ string) (string, error) {
			now := time.Now()
			zone := "Local"
			if tz := argString(args, "timezone"); tz != "" {
				loc, err := time.LoadLocation(tz)
				if err != nil {
					return "", fmt.Errorf("unknown timezone %q", tz)
				}
				now, zone = now.In(loc), tz
			}
			switch argString(args, "format") {
			case "unix":
				return strconv.FormatInt(now.Unix(), 10), nil
			case "date":
				return now.Format("2006-01-02") + " (" + now.Weekday().String() + ")", nil
			case "time":
				return now.Format("15:04:05 MST"), nil
			}
			return fmt.Sprintf("%s (%s, %s)", now.Format(time.RFC3339), now.Weekday(), zone), nil
		},
	}
}
