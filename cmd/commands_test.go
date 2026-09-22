package cmd

import (
	"strings"
	"testing"
)

func TestCommandTableIsConsistent(t *testing.T) {
	seen := map[string]bool{}
	group := ""
	groupsSeen := map[string]bool{}
	for _, c := range commandTable() {
		if !strings.HasPrefix(c.name, "/") || c.summary == "" || c.group == "" || c.run == nil {
			t.Errorf("incomplete command row %+v", c)
		}
		if seen[c.name] {
			t.Errorf("duplicate command %s", c.name)
		}
		seen[c.name] = true
		if c.group != group {
			if groupsSeen[c.group] {
				t.Errorf("group %s is not contiguous", c.group)
			}
			groupsSeen[c.group] = true
			group = c.group
		}
	}
	completions := map[string]bool{}
	for _, sc := range GetSlashCommands() {
		completions[strings.Fields(sc.Command)[0]] = true
	}
	for name := range seen {
		if !completions[name] {
			t.Errorf("%s has no completer entry", name)
		}
	}
	for name := range completions {
		if !seen[name] {
			t.Errorf("the completer lists %s, which is not a command", name)
		}
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	text := helpText()
	for _, c := range commandTable() {
		if !strings.Contains(text, c.name+" ") && !strings.Contains(text, c.name+"\n") {
			t.Errorf("/help does not list %s", c.name)
		}
	}
	if !strings.Contains(text, "exit / quit") {
		t.Error("/help does not list exit")
	}
}

func TestDispatchUnknownCommand(t *testing.T) {
	r := &repl{}
	out := captureStdoutForTest(t, func() { r.dispatch("/nope now") })
	if !strings.Contains(out, "Unknown command: /nope") {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestDispatchRunsHelpWithoutAnAssistant(t *testing.T) {
	r := &repl{}
	out := captureStdoutForTest(t, func() { r.dispatch("/help") })
	if !strings.Contains(out, "/mode") {
		t.Fatalf("help output missing /mode: %q", out)
	}
}
