package main

import "strings"

func withStage(line string) string {
	if line == "" || stageNamed(line) {
		return line
	}
	return line + ": " + findingStage(line)
}

func stageNamed(line string) bool {
	for _, stage := range []string{"parameter", "policy", "catalog", "upstream", "missing auth", "held until you approve", "token url is unset"} {
		if strings.Contains(line, stage) {
			return true
		}
	}
	return false
}

func findingStage(line string) string {
	switch {
	case strings.Contains(line, "unset"), strings.Contains(line, "no env var"):
		return "missing auth"
	case strings.Contains(line, "approval"):
		return "held until you approve"
	case strings.Contains(line, "confirmation"), strings.Contains(line, "destructive"):
		return "policy"
	default:
		return "catalog"
	}
}

func withStages(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = withStage(line)
	}
	return out
}
