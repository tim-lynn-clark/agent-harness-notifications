package classify

import "strings"

// EndsWithQuestion implements the end-of-turn question heuristic (spec §5.2).
func EndsWithQuestion(msg string) bool {
	var lines []string
	inFence := false
	for _, line := range strings.Split(strings.ReplaceAll(msg, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			lines = append(lines, "") // a removed block still separates paragraphs
			continue
		}
		if inFence {
			continue
		}
		lines = append(lines, line)
	}

	var last []string
	var current []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if len(current) > 0 {
				last, current = current, nil
			}
			continue
		}
		current = append(current, line)
	}
	if len(current) > 0 {
		last = current
	}

	for _, line := range last {
		line = strings.TrimRight(line, " \t*_)\"'`>")
		if strings.HasSuffix(line, "?") || strings.HasSuffix(line, "？") {
			return true
		}
	}
	return false
}
