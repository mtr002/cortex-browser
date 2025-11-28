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

func ParseGoalWithLLM(client *LLMClient, goal string, pageContext *PageContext) (*CommandSequence, error) {
	prompt := BuildGoalParsingPrompt(goal, pageContext)

	log.Printf("LLM Parsing goal: %s", goal)

	response, err := client.Generate(prompt)
	if err != nil {
		return nil, fmt.Errorf("LLM generation failed: %v", err)
	}

	log.Printf("LLM Response: %s", response)

	jsonStr := extractJSON(response)
	if jsonStr == "" {
		return nil, fmt.Errorf("no valid JSON found in LLM response")
	}

	var parsedGoal ParsedGoal
	if err := json.Unmarshal([]byte(jsonStr), &parsedGoal); err != nil {
		log.Printf("Failed to parse as single JSON, trying to merge multiple objects")
		mergedJSON := extractAndMergeJSON(response)
		if mergedJSON != "" {
			if err := json.Unmarshal([]byte(mergedJSON), &parsedGoal); err != nil {
				return nil, fmt.Errorf("failed to parse merged LLM JSON: %v", err)
			}
		} else {
			return nil, fmt.Errorf("failed to parse LLM JSON: %v", err)
		}
	}

	sequence := convertToCommandSequence(&parsedGoal)

	if sequence == nil {
		return nil, fmt.Errorf("LLM generated no valid commands after filtering invalid actions")
	}

	log.Printf("LLM Parsed into %d commands with confidence %.2f", len(sequence.Commands), parsedGoal.Confidence)

	return sequence, nil
}

func extractJSON(response string) string {
	codeBlockRegex := regexp.MustCompile("```(?:json)?\\s*([\\s\\S]*?)```")
	matches := codeBlockRegex.FindStringSubmatch(response)
	if len(matches) > 1 {
		return extractFirstJSON(matches[1])
	}

	return extractFirstJSON(response)
}

func extractFirstJSON(text string) string {
	text = strings.TrimSpace(text)

	startIdx := strings.Index(text, "{")
	if startIdx == -1 {
		return ""
	}

	braceCount := 0
	for i := startIdx; i < len(text); i++ {
		if text[i] == '{' {
			braceCount++
		} else if text[i] == '}' {
			braceCount--
			if braceCount == 0 {
				return text[startIdx : i+1]
			}
		}
	}

	if startIdx < len(text) {
		endIdx := strings.Index(text[startIdx:], "\n\n")
		if endIdx > 0 {
			return text[startIdx : startIdx+endIdx]
		}
		return text[startIdx:]
	}

	return ""
}

func extractAndMergeJSON(response string) string {
	var jsonObjects []ParsedGoal
	text := strings.TrimSpace(response)

	startIdx := 0
	for startIdx < len(text) {
		idx := strings.Index(text[startIdx:], "{")
		if idx == -1 {
			break
		}
		actualStart := startIdx + idx

		braceCount := 0
		endIdx := -1
		for i := actualStart; i < len(text); i++ {
			if text[i] == '{' {
				braceCount++
			} else if text[i] == '}' {
				braceCount--
				if braceCount == 0 {
					endIdx = i + 1
					break
				}
			}
		}

		if endIdx > actualStart {
			jsonStr := text[actualStart:endIdx]
			var obj ParsedGoal
			if err := json.Unmarshal([]byte(jsonStr), &obj); err == nil {
				jsonObjects = append(jsonObjects, obj)
			}
			startIdx = endIdx
		} else {
			break
		}
	}

	if len(jsonObjects) == 0 {
		return ""
	}

	merged := ParsedGoal{
		Intent:     "multi_step",
		Steps:      []LLMStep{},
		Confidence: 0.0,
	}

	for _, obj := range jsonObjects {
		merged.Steps = append(merged.Steps, obj.Steps...)
		if obj.Confidence > merged.Confidence {
			merged.Confidence = obj.Confidence
		}
	}

	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		return ""
	}

	log.Printf("Merged %d JSON objects into one with %d total steps", len(jsonObjects), len(merged.Steps))
	return string(mergedJSON)
}

func convertToCommandSequence(parsed *ParsedGoal) *CommandSequence {
	commands := []CommandPayload{}
	validActions := map[string]bool{
		"navigate":    true,
		"input":       true,
		"click":       true,
		"get_content": true,
	}

	for _, step := range parsed.Steps {
		if !validActions[step.Action] {
			log.Printf("Filtering out invalid action: %s", step.Action)
			continue
		}

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

	if len(commands) == 0 {
		log.Printf("No valid commands after filtering invalid actions")
		return nil
	}

	commands = postProcessCommands(commands)

	return &CommandSequence{
		Commands: commands,
		Total:    len(commands),
		Current:  0,
	}
}

func postProcessCommands(commands []CommandPayload) []CommandPayload {
	filtered := []CommandPayload{}

	for i, cmd := range commands {
		if cmd.Action == "navigate" && cmd.URL != "" {
			if strings.Contains(cmd.URL, "example.com") || strings.Contains(cmd.URL, "checkout") {
				log.Printf("Removing hallucinated navigation: %s", cmd.URL)
				continue
			}
		}

		if cmd.Action == "click" && cmd.Selector != "" {
			if strings.Contains(cmd.Selector, "example") {
				log.Printf("Removing invalid selector: %s", cmd.Selector)
				continue
			}
		}

		if cmd.Action == "get_content" && i == 0 && len(commands) > 1 {
			if i+1 < len(commands) && commands[i+1].Action == "click" {
				log.Printf("Removing unnecessary get_content before click")
				continue
			}
		}

		filtered = append(filtered, cmd)
	}

	if len(filtered) == 0 {
		log.Printf("Post-processing removed all commands, using original")
		return commands
	}

	return filtered
}

func ShouldUseLLM(goal string) bool {
	goal = strings.ToLower(strings.TrimSpace(goal))

	ambiguousKeywords := []string{
		"find", "get", "show", "look for", "look up",
		"what is", "tell me", "help me", "can you",
		"i want", "i need", "please",
		"select", "choose", "pick",
	}

	for _, keyword := range ambiguousKeywords {
		if strings.Contains(goal, keyword) {
			return true
		}
	}

	if len(goal) > 80 {
		return true
	}

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

	if !hasSimplePattern && len(goal) > 30 {
		return true
	}

	return false
}
