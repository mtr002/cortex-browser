package llm

import "fmt"

// BuildGoalParsingPrompt creates a prompt for parsing user goals into browser commands
func BuildGoalParsingPrompt(goal string, pageContext *PageContext) string {
	basePrompt := `You are an intelligent browser automation assistant. Parse the user's goal into executable browser commands.

IMPORTANT: Return ONLY the JSON object for the user's goal. Do NOT include examples or additional text.

User Goal: "%s"

Return ONLY this JSON structure (no markdown code blocks, no explanations, no examples):
{
  "intent": "navigate|search|click|input|get_content|multi_step",
  "steps": [
    {"action": "navigate", "url": "https://example.com"},
    {"action": "input", "selector": "input[name='q']", "text": "search term"},
    {"action": "click", "selector": "button[type='submit']"},
    {"action": "get_content"}
  ],
  "confidence": 0.95
}

Available actions:
- "navigate": Navigate to a URL (requires "url" field)
- "input": Type text into an input field (requires "selector" and "text" fields)
- "click": Click an element (requires "selector" field)
- "get_content": Extract page content (no additional fields)

Rules:
- For search goals like "find X" or "search for X" or "look for X": navigate to google.com → input X → click search button
- For "look for X on Y.com" or "search for X on Y.com": navigate to Y.com → input X in search box → click search button
- For e-commerce sites (amazon.com, ebay.com, etc.): "look for X" means navigate → search for X
- For navigation goals: extract URL or use common site names (google.com, github.com, amazon.com, etc.)
- For ambiguous goals: interpret intent and create appropriate steps
- Use google.com as default search engine if no site specified
- Use input[name='q'] or textarea[name='q'] for Google search box
- Use input[name='field-keywords'] for Amazon search box
- Use button[name='btnK'] or input[type='submit'] for Google search button
- Use input[type='submit'][value='Go'] or button for Amazon search

Return ONLY the JSON object, nothing else:`

	// Add page context if available
	if pageContext != nil && pageContext.URL != "" {
		contextInfo := fmt.Sprintf(`

Current Page Context:
- URL: %s
- Title: %s
- Content Type: %s`, pageContext.URL, pageContext.Title, pageContext.ContentType)

		basePrompt += contextInfo
	}

	basePrompt += fmt.Sprintf("\n\nUser Goal: %s\n\nReturn JSON:", goal)

	return basePrompt
}

// PageContext provides context about the current page
type PageContext struct {
	URL         string
	Title       string
	ContentType string // "search", "form", "navigation", "general"
	Elements    []ElementInfo
}

// ElementInfo describes a page element
type ElementInfo struct {
	Tag      string
	Type     string
	ID       string
	Name     string
	Text     string
	Selector string
}
