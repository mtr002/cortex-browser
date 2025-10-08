package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/websocket"
)

// Message types
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type ExecuteTaskPayload struct {
	Goal string `json:"goal"`
}

type CommandPayload struct {
	Action   string `json:"action"`
	URL      string `json:"url,omitempty"`
	Selector string `json:"selector,omitempty"`
	Text     string `json:"text,omitempty"`
}

type PageContentPayload struct {
	HTML       string `json:"html"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	Text       string `json:"text"`
	ReadyState string `json:"readyState"`
}

type ContentAnalysisResult struct {
	Selectors   []string `json:"selectors"`
	Suggestions []string `json:"suggestions"`
	ContentType string   `json:"contentType"`
}

type TaskCompletePayload struct {
	Message string `json:"message"`
}

type ErrorPayload struct {
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// TODO: Add proper origin checking for production
		return true
	},
}

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer conn.Close()

	log.Println("New client connected")

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			return
		}

		log.Printf("Received: %s", string(messageBytes))

		// Handle the message with connection for completion messages
		if err := handleMessageWithConnection(conn, messageBytes); err != nil {
			log.Println("Message handling error:", err)
			return
		}
	}
}

func handleMessageWithConnection(conn *websocket.Conn, messageBytes []byte) error {
	var msg Message
	if err := json.Unmarshal(messageBytes, &msg); err != nil {
		log.Println("JSON unmarshal error:", err)
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Invalid JSON format",
				Code:    "PARSE_ERROR",
			},
		})
	}

	switch msg.Type {
	case "HANDSHAKE":
		// No response needed for handshake
		log.Println("Handshake received from extension")
		return nil
	case "EXECUTE_TASK":
		return handleExecuteTaskWithCompletion(conn, msg.Payload)
	case "PAGE_CONTENT":
		return handlePageContent(conn, msg.Payload)
	default:
		log.Printf("Unknown message type: %s", msg.Type)
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Unknown message type",
				Code:    "UNKNOWN_TYPE",
			},
		})
	}
}

func handleHandshake(payload interface{}) *Message {
	log.Println("Handshake received from extension")
	return nil // No response needed for handshake
}

// Task completion handling functions

func handleExecuteTaskWithCompletion(conn *websocket.Conn, payload interface{}) error {
	// Parse the task payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Failed to parse task payload",
				Code:    "PAYLOAD_ERROR",
			},
		})
	}

	var taskPayload ExecuteTaskPayload
	if err := json.Unmarshal(payloadBytes, &taskPayload); err != nil {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Invalid task payload format",
				Code:    "TASK_FORMAT_ERROR",
			},
		})
	}

	log.Printf("Processing goal: %s", taskPayload.Goal)

	// Simple goal-to-command mapping
	command := parseGoalToCommand(taskPayload.Goal)
	if command == nil {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Could not understand the goal",
				Code:    "GOAL_PARSE_ERROR",
			},
		})
	}

	// Send the command
	if err := sendMessage(conn, &Message{
		Type:    "COMMAND",
		Payload: command,
	}); err != nil {
		return err
	}

	// Send completion message after a short delay to allow command execution
	go func() {
		// Wait for command to execute (adjust timing as needed)
		if command.Action == "navigate" {
			// Navigation takes longer
			time.Sleep(2 * time.Second)
		} else {
			time.Sleep(1 * time.Second)
		}

		// Send task completion
		sendMessage(conn, &Message{
			Type: "TASK_COMPLETE",
			Payload: TaskCompletePayload{
				Message: fmt.Sprintf("Successfully executed: %s", taskPayload.Goal),
			},
		})
	}()

	return nil
}

func sendMessage(conn *websocket.Conn, message *Message) error {
	responseBytes, err := json.Marshal(message)
	if err != nil {
		log.Println("JSON marshal error:", err)
		return err
	}

	if err := conn.WriteMessage(websocket.TextMessage, responseBytes); err != nil {
		log.Println("Write error:", err)
		return err
	}

	log.Printf("Sent: %s", string(responseBytes))
	return nil
}

func parseGoalToCommand(goal string) *CommandPayload {
	goal = strings.ToLower(strings.TrimSpace(goal))
	log.Printf("Parsing goal: %s", goal)

	// Enhanced rule-based command generation
	if containsNavigationKeywords(goal) {
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	// Handle page content requests
	if containsContentKeywords(goal) {
		return &CommandPayload{
			Action: "get_content",
		}
	}

	// Handle search commands
	if containsSearchKeywords(goal) {
		return &CommandPayload{
			Action:   "input",
			Selector: "input[type='search'], input[name='q'], #search, [role='searchbox']",
			Text:     extractSearchTermFromGoal(goal),
		}
	}

	// Handle click commands
	if containsClickKeywords(goal) {
		return &CommandPayload{
			Action:   "click",
			Selector: extractSelectorFromGoal(goal),
		}
	}

	// Multi-step goals (navigate and search)
	if containsNavigationKeywords(goal) && containsSearchKeywords(goal) {
		// For now, handle navigation first
		// TODO: Implement multi-step command sequences
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	// Default: try to navigate if it looks like a URL
	if containsURL(goal) {
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	return nil
}

func extractURLFromGoal(goal string) string {
	// Enhanced URL extraction with regex
	urlRegex := regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?([a-zA-Z0-9-]+\.(?:com|org|net|edu|gov|io|co))(?:/[^\s]*)?`)
	match := urlRegex.FindString(goal)
	if match != "" {
		if !strings.HasPrefix(match, "http") {
			return "https://" + match
		}
		return match
	}

	// Fallback to simple word extraction
	words := strings.Fields(goal)
	for _, word := range words {
		if containsURL(word) {
			if !strings.HasPrefix(word, "http") {
				return "https://" + word
			}
			return word
		}
	}

	// Common site mappings
	siteMap := map[string]string{
		"google":   "https://google.com",
		"github":   "https://github.com",
		"youtube":  "https://youtube.com",
		"facebook": "https://facebook.com",
		"twitter":  "https://twitter.com",
		"linkedin": "https://linkedin.com",
	}

	for site, url := range siteMap {
		if strings.Contains(goal, site) {
			return url
		}
	}

	return "https://google.com" // fallback
}

func extractSelectorFromGoal(goal string) string {
	// Simple selector extraction (could be enhanced)
	if strings.Contains(goal, "button") {
		return "button"
	}
	if strings.Contains(goal, "link") {
		return "a"
	}
	return "*" // fallback selector
}

func extractSearchTermFromGoal(goal string) string {
	// Extract search term after "search for" or similar patterns
	goal = strings.ToLower(goal)

	patterns := []string{"search for ", "search ", "find ", "look for "}
	for _, pattern := range patterns {
		if idx := strings.Index(goal, pattern); idx != -1 {
			term := goal[idx+len(pattern):]
			return strings.TrimSpace(term)
		}
	}

	// If no pattern found, return the whole goal as search term
	return strings.TrimSpace(goal)
}

// New helper functions for enhanced goal parsing
func containsNavigationKeywords(goal string) bool {
	keywords := []string{"navigate", "go to", "visit", "open", "browse to"}
	for _, keyword := range keywords {
		if strings.Contains(goal, keyword) {
			return true
		}
	}
	return false
}

func containsContentKeywords(goal string) bool {
	keywords := []string{"get content", "page content", "read page", "extract content", "analyze page"}
	for _, keyword := range keywords {
		if strings.Contains(goal, keyword) {
			return true
		}
	}
	return false
}

func containsSearchKeywords(goal string) bool {
	keywords := []string{"search", "find", "look for", "type"}
	for _, keyword := range keywords {
		if strings.Contains(goal, keyword) {
			return true
		}
	}
	return false
}

func containsClickKeywords(goal string) bool {
	keywords := []string{"click", "press", "tap", "select"}
	for _, keyword := range keywords {
		if strings.Contains(goal, keyword) {
			return true
		}
	}
	return false
}

func containsURL(goal string) bool {
	// Check for common URL patterns
	urlPatterns := []string{".com", ".org", ".net", ".edu", ".gov", "http", "www."}
	for _, pattern := range urlPatterns {
		if strings.Contains(goal, pattern) {
			return true
		}
	}
	return false
}

// Page content handler for Phase 4
func handlePageContent(conn *websocket.Conn, payload interface{}) error {
	// Parse the page content payload
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Failed to parse page content payload",
				Code:    "PAYLOAD_ERROR",
			},
		})
	}

	var contentPayload PageContentPayload
	if err := json.Unmarshal(payloadBytes, &contentPayload); err != nil {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Invalid page content format",
				Code:    "CONTENT_FORMAT_ERROR",
			},
		})
	}

	log.Printf("Analyzing page content from: %s", contentPayload.URL)

	// Analyze the HTML content with goquery
	analysis, err := analyzePageContent(contentPayload.HTML)
	if err != nil {
		log.Printf("Failed to analyze page content: %v", err)
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Failed to analyze page content",
				Code:    "ANALYSIS_ERROR",
			},
		})
	}

	// Send analysis result back
	return sendMessage(conn, &Message{
		Type:    "CONTENT_ANALYSIS",
		Payload: analysis,
	})
}

// Analyze page content using goquery
func analyzePageContent(htmlContent string) (*ContentAnalysisResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %v", err)
	}

	result := &ContentAnalysisResult{
		Selectors:   []string{},
		Suggestions: []string{},
	}

	// Find interactive elements
	doc.Find("input, button, a, select, textarea").Each(func(i int, s *goquery.Selection) {
		// Generate selector for this element
		selector := generateSmartSelector(s)
		if selector != "" {
			result.Selectors = append(result.Selectors, selector)
		}
	})

	// Determine content type
	result.ContentType = determineContentType(doc)

	// Generate suggestions based on content
	result.Suggestions = generateActionSuggestions(doc)

	return result, nil
}

// Generate smart CSS selector for an element
func generateSmartSelector(s *goquery.Selection) string {
	// Try ID first
	if id, exists := s.Attr("id"); exists && id != "" {
		return "#" + id
	}

	// Try name attribute
	if name, exists := s.Attr("name"); exists && name != "" {
		return "[name='" + name + "']"
	}

	// Try class if it's specific
	if class, exists := s.Attr("class"); exists && class != "" {
		classes := strings.Fields(class)
		if len(classes) == 1 {
			return "." + classes[0]
		}
	}

	// Try role attribute
	if role, exists := s.Attr("role"); exists && role != "" {
		return "[role='" + role + "']"
	}

	// Fall back to tag name with type if available
	tagName := goquery.NodeName(s)
	if tagType, exists := s.Attr("type"); exists && tagType != "" {
		return tagName + "[type='" + tagType + "']"
	}

	return tagName
}

// Determine the type of content on the page
func determineContentType(doc *goquery.Document) string {
	// Check for search pages
	if doc.Find("input[type='search'], input[name='q'], [role='searchbox']").Length() > 0 {
		return "search"
	}

	// Check for forms
	if doc.Find("form").Length() > 0 {
		return "form"
	}

	// Check for navigation
	if doc.Find("nav, .navigation, .menu").Length() > 0 {
		return "navigation"
	}

	return "general"
}

// Generate action suggestions based on page content
func generateActionSuggestions(doc *goquery.Document) []string {
	var suggestions []string

	// Search suggestions
	if doc.Find("input[type='search'], input[name='q']").Length() > 0 {
		suggestions = append(suggestions, "Search for something")
	}

	// Link suggestions
	linkCount := doc.Find("a[href]").Length()
	if linkCount > 0 {
		suggestions = append(suggestions, fmt.Sprintf("Click on one of %d links", linkCount))
	}

	// Button suggestions
	buttonCount := doc.Find("button, input[type='submit'], input[type='button']").Length()
	if buttonCount > 0 {
		suggestions = append(suggestions, fmt.Sprintf("Click on one of %d buttons", buttonCount))
	}

	return suggestions
}

func main() {
	flag.Parse()

	http.HandleFunc("/ws", handler)
	log.Println("Cortex Backend started on port 8080")
	log.Println("WebSocket endpoint: ws://localhost:8080/ws")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
