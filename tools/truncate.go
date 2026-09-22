package tools

import "fmt"

// truncateOutput caps content near limit bytes, keeping the head and tail
// thirds with an omission marker between them.
func truncateOutput(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	head := limit * 2 / 3
	tail := limit / 3
	omitted := len(s) - head - tail
	return s[:head] + fmt.Sprintf("\n... [%d bytes truncated] ...\n", omitted) + s[len(s)-tail:], true
}
