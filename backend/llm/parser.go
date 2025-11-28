package llm

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// ParsedGoal represents the LLM's parsed response
type ParsedGoal struct {
	Intent     string    `json:"intent"`
	Steps      []LLMStep `json:"steps"`
	Confidence float64   `json:"confidence"`
}

// LLMStep represents a single step in the parsed goal
type LLMStep struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
}

// CommandPayload matches the main package structure (exported for conversion)
type CommandPayload struct {
	Action   string
	URL      string
	Selector string
	Text     string
}

// CommandSequence matches the main package structure (exported for conversion)
type CommandSequence struct {
	Commands []CommandPayload
	TaskID   string
	Total    int
	Current  int
}

// ParseGoalWithLLM uses LLM to parse a user goal into command sequence
func ParseGoalWithLLM(client *LLMClient, goal string, pageContext *PageContext) (*CommandSequence, error) {
	// Build prompt
	prompt := BuildGoalParsingPrompt(goal, pageContext)

	log.Printf("🤖 LLM Parsing goal: %s", goal)

	// Get LLM response
	response, err := client.Generate(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %v", err)
	}

	log.Printf("🤖 LLM Response: %s", response)

	// Extract JSON from response (might have extra text)
	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no valid JSON found in LLM response")
	}

	// Parse JSON
	var parsedGoal ParsedGoal
	if err := json.Unmarshal([]byte(jsonStr), &parsedGoal); err != nil {
		return nil, fmt.Errorf("failed to parse LLM JSON: %v", err)
	}

	// Convert to CommandSequence
	sequence := convertToCommandSequence(&parsedGoal)

	log.Printf("🤖 LLM Parsed into %d commands with confidence %.2f", len(sequence.Commands), parsedGoal.Confidence)

	return sequence, nil
}

// extractJSON extracts the first valid JSON object from LLM response
func extractJSON(response string) string {
	// Try to find JSON in code blocks first
	codeBlockRegex := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")
	matches := codeBlockRegex.FindStringSubmatch(response)
	if len(matches) > 1 {
		// Extract first JSON from code block
		return extractFirstJSON(matches[1])
	}

	// Try to find first JSON object directly
	return extractFirstJSON(response)
}

// extractFirstJSON finds the first complete JSON object in text
func extractFirstJSON(text string) string {
	text = strings.TrimSpace(text)

	// Find the first opening brace
	startIdx := strings.Index(text, "{")
	if startIdx == -1 {
		return ""
	}

	// Find the matching closing brace by counting braces
	braceCount := 0
	for i := startIdx; i < len(text); i++ {
		if text[i] == '{' {
			braceCount++
		} else if text[i] == '}' {
			braceCount--
			if braceCount == 0 {
				// Found matching closing brace
				return text[startIdx : i+1]
			}
		}
	}

	// If no matching brace found, try to parse what we have
	// This handles cases where JSON might be cut off
	if startIdx < len(text) {
		// Try to find end by looking for newline or next JSON object
		endIdx := strings.Index(text[startIdx:], "\n\n")
		if endIdx > 0 {
			return text[startIdx : startIdx+endIdx]
		}
		return text[startIdx:]
	}

	return ""
}

// convertToCommandSequence converts ParsedGoal to CommandSequence
func convertToCommandSequence(parsed *ParsedGoal) *CommandSequence {
	commands := []CommandPayload{}

	for _, step := range parsed.Steps {
		cmd := CommandPayload{
			Action: step.Action,
		}

		switch step.Action {
		case "navigate":
			cmd.URL = step.URL
		case "input":
			cmd.Selector = step.Selector
			cmd.Text = step.Text
		case "click":
			cmd.Selector = step.Selector
		case "get_content":
			// No additional fields needed
		}

		commands = append(commands, cmd)
	}

	return &CommandSequence{
		Commands: commands,
		Total:    len(commands),
		Current:  0,
	}
}

// ShouldUseLLM determines if a goal should use LLM parsing
func ShouldUseLLM(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))

	// Use LLM for ambiguous goals
	ambiguousKeywords := []string{
		"find", "get", "show", "look for", "look up",
		"what is", "tell me", "help me", "can you",
		"i want", "i need", "please",
	}

	for _, keyword := range ambiguousKeywords {
		if strings.Contains(goal, keyword) {
			return true
		}
	}

	// Use LLM for long/complex goals
	if len(goal) > 80 {
		return true
	}

	// Use LLM if goal doesn't match simple patterns
	simplePatterns := []string{
		"navigate to", "go to", "visit",
		"search for", "click", "type",
	}

	hasSimplePattern := false
	for _, pattern := range simplePatterns {
		if strings.Contains(goal, pattern) {
			hasSimplePattern = true
			break
		}
	}

	// If no simple pattern and goal is complex, use LLM
	if !hasSimplePattern && len(goal) > 30 {
		return true
	}

	return false
}
