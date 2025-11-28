package llm

import "fmt"

// BuildGoalParsingPrompt creates a prompt for parsing user goals into browser commands
func BuildGoalParsingPrompt(goal string, pageContext *PageContext) string {
	basePrompt := `You are an intelligent browser automation assistant. Parse the user's goal into executable browser commands.

CRITICAL: Return ONLY ONE JSON object. Put ALL steps in a single "steps" array. Do NOT return multiple JSON objects.

User Goal: "%s"

Return ONLY this SINGLE JSON structure (no markdown, no explanations, no examples, no multiple objects):
{
  "intent": "multi_step",
  "steps": [
    {"action": "navigate", "url": "https://example.com"},
    {"action": "input", "selector": "input[name='q']", "text": "search term"},
    {"action": "click", "selector": "button[type='submit']"}
  ],
  "confidence": 0.95
}

IMPORTANT: For goals like "find X on Y.com" or "search for X on Y.com", include ALL steps in ONE steps array:
- Step 1: navigate to the site
- Step 2: input the search term
- Step 3: click search button
ALL in the same JSON object's steps array.

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

Context-Aware Commands (when page context is available):
- Use page content to understand what elements are available and generate accurate selectors
- "click on X" where X is mentioned in page content: Search page content for X, generate selector for that element
- "select X": Find X in page content, click on it
- Generate selectors based on actual page structure visible in the context
- NEVER use "find", "search", "locate" actions - they don't exist
- ONLY use: "navigate", "input", "click", "get_content"

Return ONLY the JSON object, nothing else:`

	// Add page context if available
	if pageContext != nil && pageContext.URL != "" {
		contextInfo := fmt.Sprintf(`

CURRENT PAGE CONTEXT (You are currently on this page):
- URL: %s
- Title: %s
- Content Type: %s`, pageContext.URL, pageContext.Title, pageContext.ContentType)

		// Include page text for context-aware commands
		if pageContext.Text != "" && len(pageContext.Text) > 0 {
			// Include relevant page text (first 2000 chars) for understanding page content
			textPreview := pageContext.Text
			if len(textPreview) > 2000 {
				textPreview = textPreview[:2000] + "..."
			}
			contextInfo += fmt.Sprintf(`
- Page Content Preview: %s`, textPreview)
		}

		contextInfo += `

IMPORTANT: Since you have page context, use it to:
- Find specific items mentioned in the goal (e.g., product names → look for them in page content)
- Generate accurate selectors based on actual page structure
- Understand what elements are available (buttons, inputs, links)`

		basePrompt += contextInfo
	}

	basePrompt += fmt.Sprintf("\n\nUser Goal: %s\n\nReturn JSON:", goal)

	return basePrompt
}

// PageContext provides context about the current page
type PageContext struct {
	URL         string
	Title       string
	ContentType string // "search", "form", "navigation", "general", "ecommerce"
	Elements    []ElementInfo
	HTML        string // Full HTML for context-aware parsing
	Text        string // Page text content
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
