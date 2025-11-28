package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"cortex-browser/backend/llm"

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

// Multi-step task planning structures
type CommandSequence struct {
	Commands []CommandPayload `json:"commands"`
	TaskID   string           `json:"taskId"`
	Total    int              `json:"total"`
	Current  int              `json:"current"`
}

type TaskState struct {
	TaskID      string          `json:"taskId"`
	Goal        string          `json:"goal"`
	Sequence    CommandSequence `json:"sequence"`
	Status      string          `json:"status"` // "pending", "executing", "completed", "failed"
	CurrentStep int             `json:"currentStep"`
	Results     []CommandResult `json:"results"`
}

type CommandResult struct {
	Step      int    `json:"step"`
	Action    string `json:"action"`
	Success   bool   `json:"success"`
	Details   string `json:"details,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
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

// Task state management
var activeTasks = make(map[string]*TaskState)
var taskCounter int64

// LLM client (optional, for intelligent goal parsing)
var llmClient *llm.LLMClient
var useLLM bool

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
	case "COMMAND_COMPLETE":
		return handleCommandComplete(conn, msg.Payload)
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

// Handle command completion and advance sequence
func handleCommandComplete(conn *websocket.Conn, payload interface{}) error {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	var result CommandResult
	if err := json.Unmarshal(payloadBytes, &result); err != nil {
		log.Printf("Failed to parse command result: %v", err)
		return nil
	}

	// Find the task by checking active tasks
	// Look for executing task first, then any pending task
	var taskState *TaskState
	for _, task := range activeTasks {
		if task.Status == "executing" {
			taskState = task
			break
		}
	}

	// If no executing task found, try to find any active task
	if taskState == nil {
		for _, task := range activeTasks {
			if task.Status == "pending" || task.Status == "executing" {
				taskState = task
				// Set to executing if it was pending
				if taskState.Status == "pending" {
					taskState.Status = "executing"
				}
				break
			}
		}
	}

	if taskState == nil {
		log.Printf("No active task found for command completion. Active tasks: %d", len(activeTasks))
		return nil
	}

	// Update task state
	taskState.CurrentStep++
	taskState.Results = append(taskState.Results, result)

	// Check if more commands remain
	if taskState.CurrentStep < len(taskState.Sequence.Commands) {
		// Send next command
		nextCommand := taskState.Sequence.Commands[taskState.CurrentStep]
		taskState.Sequence.Current = taskState.CurrentStep

		// Update sequence progress
		if err := sendMessage(conn, &Message{
			Type:    "COMMAND_SEQUENCE_UPDATE",
			Payload: taskState.Sequence,
		}); err != nil {
			return err
		}

		// Wait a bit before next command (especially after navigation)
		if taskState.CurrentStep > 0 {
			prevCommand := taskState.Sequence.Commands[taskState.CurrentStep-1]
			if prevCommand.Action == "navigate" {
				time.Sleep(2 * time.Second)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}

		// Send next command
		return sendMessage(conn, &Message{
			Type:    "COMMAND",
			Payload: nextCommand,
		})
	} else {
		// All commands completed
		taskState.Status = "completed"
		delete(activeTasks, taskState.TaskID)

		return sendMessage(conn, &Message{
			Type: "TASK_COMPLETE",
			Payload: TaskCompletePayload{
				Message: fmt.Sprintf("Successfully completed multi-step task: %s", taskState.Goal),
			},
		})
	}
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

	// Parse goal into command sequence (supports both single and multi-step)
	sequence := parseGoalToSequence(taskPayload.Goal)
	if sequence == nil || len(sequence.Commands) == 0 {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Could not understand the goal",
				Code:    "GOAL_PARSE_ERROR",
			},
		})
	}

	// Create task state
	taskID := generateTaskID()
	taskState := &TaskState{
		TaskID:      taskID,
		Goal:        taskPayload.Goal,
		Sequence:    *sequence,
		Status:      "pending",
		CurrentStep: 0,
		Results:     []CommandResult{},
	}
	activeTasks[taskID] = taskState

	// Set task ID in sequence
	sequence.TaskID = taskID

	// If single command, send it directly (backward compatible)
	if len(sequence.Commands) == 1 {
		taskState.Status = "executing"
		sequence.Current = 0
		sequence.Total = 1

		command := sequence.Commands[0]
		if err := sendMessage(conn, &Message{
			Type:    "COMMAND",
			Payload: command,
		}); err != nil {
			return err
		}

		// For single commands, we still wait for COMMAND_COMPLETE
		// The completion handler will send TASK_COMPLETE
	} else {
		// Multi-step sequence - send sequence info and first command
		taskState.Status = "executing"
		sequence.TaskID = taskID
		sequence.Current = 0
		sequence.Total = len(sequence.Commands)

		// Send sequence start notification
		if err := sendMessage(conn, &Message{
			Type:    "COMMAND_SEQUENCE",
			Payload: sequence,
		}); err != nil {
			return err
		}

		// Send first command
		if err := sendMessage(conn, &Message{
			Type:    "COMMAND",
			Payload: sequence.Commands[0],
		}); err != nil {
			return err
		}
	}

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

// Generate unique task ID
func generateTaskID() string {
	counter := atomic.AddInt64(&taskCounter, 1)
	return fmt.Sprintf("task_%d_%d", time.Now().Unix(), counter)
}

// Parse goal into command sequence (supports multi-step and LLM)
func parseGoalToSequence(goal string) *CommandSequence {
	originalGoal := goal
	goal = strings.ToLower(strings.TrimSpace(goal))
	log.Printf("Parsing goal to sequence: %s", goal)

	// Try LLM first if enabled and goal is complex/ambiguous
	if useLLM && llmClient != nil && llm.ShouldUseLLM(originalGoal) {
		log.Println("🤖 Using LLM for goal parsing")
		llmSequence, err := llm.ParseGoalWithLLM(llmClient, originalGoal, nil)
		if err != nil {
			log.Printf("⚠️  LLM parsing failed: %v, falling back to rules", err)
		} else if llmSequence != nil && len(llmSequence.Commands) > 0 {
			// Convert LLM sequence to main package sequence
			commands := make([]CommandPayload, len(llmSequence.Commands))
			for i, cmd := range llmSequence.Commands {
				commands[i] = CommandPayload{
					Action:   cmd.Action,
					URL:      cmd.URL,
					Selector: cmd.Selector,
					Text:     cmd.Text,
				}
			}
			return &CommandSequence{
				Commands: commands,
				Total:    len(commands),
				Current:  0,
			}
		}
		// Fall through to rule-based if LLM fails
	}

	// Rule-based parsing (existing logic)
	commands := []CommandPayload{}

	// Check for multi-step patterns
	// Pattern 1: "Navigate to X and search for Y"
	if strings.Contains(goal, " and ") || strings.Contains(goal, ", then ") || strings.Contains(goal, " then ") {
		commands = parseMultiStepGoal(goal)
	} else {
		// Single command - parse as before
		command := parseSingleCommand(goal)
		if command != nil {
			commands = []CommandPayload{*command}
		}
	}

	if len(commands) == 0 {
		return nil
	}

	return &CommandSequence{
		Commands: commands,
		Total:    len(commands),
		Current:  0,
	}
}

// Parse multi-step goals
func parseMultiStepGoal(goal string) []CommandPayload {
	commands := []CommandPayload{}

	// Split by common conjunctions
	parts := regexp.MustCompile(`\s+(and|then|, then|, and)\s+`).Split(goal, -1)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		command := parseSingleCommand(part)
		if command != nil {
			commands = append(commands, *command)

			// If it's a search/input command, automatically add a click on search button
			if command.Action == "input" && containsSearchKeywords(part) {
				// Add click command for search button
				searchButtonCommand := &CommandPayload{
					Action:   "click",
					Selector: "input[type='submit'], button[type='submit'], button[name='btnK'], button[name='btnG'], [aria-label*='Search' i], [value*='Search' i]",
				}
				commands = append(commands, *searchButtonCommand)
			}
		}
	}

	return commands
}

// Parse single command (original parseGoalToCommand logic)
func parseSingleCommand(goal string) *CommandPayload {
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
	// Note: For multi-step goals, the click will be added in parseMultiStepGoal
	// For single commands, we'll let the content script handle it or user can explicitly click
	if containsSearchKeywords(goal) {
		return &CommandPayload{
			Action:   "input",
			Selector: "input[name='q'], textarea[name='q'], input[type='search'], input[type='text'][name='q'], #search, [role='searchbox']",
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
	// Check if LLM should be enabled
	useLLM = os.Getenv("USE_LLM") == "true" || os.Getenv("USE_LLM") == "1"
	llmModel := os.Getenv("LLM_MODEL")
	if llmModel == "" {
		llmModel = "mistral:latest"
	}

	if useLLM {
		log.Println("🤖 Initializing LLM client...")
		llmClient = llm.NewLLMClient(llmModel)

		// Test connection
		if err := llmClient.TestConnection(); err != nil {
			log.Printf("⚠️  LLM not available: %v", err)
			log.Println("   Continuing with rule-based parsing only")
			log.Println("   To enable LLM: Start Ollama (ollama serve) and set USE_LLM=true")
			useLLM = false
		} else {
			log.Printf("✅ LLM enabled with model: %s", llmModel)
		}
	} else {
		log.Println("📝 Using rule-based parsing (set USE_LLM=true to enable AI)")
	}

	flag.Parse()

	http.HandleFunc("/ws", handler)
	log.Println("Cortex Backend started on port 8080")
	log.Println("WebSocket endpoint: ws://localhost:8080/ws")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
