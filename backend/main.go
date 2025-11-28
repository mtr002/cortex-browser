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
		return true
	},
}

var activeTasks = make(map[string]*TaskState)
var taskCounter int64
var llmClient *llm.LLMClient
var useLLM bool
var pageContexts = make(map[*websocket.Conn]*llm.PageContext)

func handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("WebSocket upgrade error:", err)
		return
	}
	defer func() {
		conn.Close()
		delete(pageContexts, conn)
	}()

	log.Println("New client connected")

	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			return
		}

		log.Printf("Received: %s", string(messageBytes))

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

	var taskState *TaskState
	for _, task := range activeTasks {
		if task.Status == "executing" {
			taskState = task
			break
		}
	}

	if taskState == nil {
		for _, task := range activeTasks {
			if task.Status == "pending" || task.Status == "executing" {
				taskState = task
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

	taskState.CurrentStep++
	taskState.Results = append(taskState.Results, result)

	if taskState.CurrentStep < len(taskState.Sequence.Commands) {
		nextCommand := taskState.Sequence.Commands[taskState.CurrentStep]
		taskState.Sequence.Current = taskState.CurrentStep

		if err := sendMessage(conn, &Message{
			Type:    "COMMAND_SEQUENCE_UPDATE",
			Payload: taskState.Sequence,
		}); err != nil {
			return err
		}

		if taskState.CurrentStep > 0 {
			prevCommand := taskState.Sequence.Commands[taskState.CurrentStep-1]
			if prevCommand.Action == "navigate" {
				time.Sleep(2 * time.Second)
			} else {
				time.Sleep(500 * time.Millisecond)
			}
		}

		return sendMessage(conn, &Message{
			Type:    "COMMAND",
			Payload: nextCommand,
		})
	} else {
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

func handleExecuteTaskWithCompletion(conn *websocket.Conn, payload interface{}) error {
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

	sequence := parseGoalToSequence(taskPayload.Goal, conn)
	if sequence == nil || len(sequence.Commands) == 0 {
		return sendMessage(conn, &Message{
			Type: "ERROR",
			Payload: ErrorPayload{
				Message: "Could not understand the goal",
				Code:    "GOAL_PARSE_ERROR",
			},
		})
	}

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

	sequence.TaskID = taskID

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

	} else {
		taskState.Status = "executing"
		sequence.TaskID = taskID
		sequence.Current = 0
		sequence.Total = len(sequence.Commands)

		if err := sendMessage(conn, &Message{
			Type:    "COMMAND_SEQUENCE",
			Payload: sequence,
		}); err != nil {
			return err
		}

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

func generateTaskID() string {
	counter := atomic.AddInt64(&taskCounter, 1)
	return fmt.Sprintf("task_%d_%d", time.Now().Unix(), counter)
}

func parseGoalToSequence(goal string, conn *websocket.Conn) *CommandSequence {
	originalGoal := goal
	goal = strings.ToLower(strings.TrimSpace(goal))
	log.Printf("Parsing goal to sequence: %s", goal)

	var pageContext *llm.PageContext
	if conn != nil {
		pageContext = pageContexts[conn]
		if pageContext != nil {
			log.Printf("Using stored page context: %s (Title: %s)", pageContext.URL, pageContext.Title)
		} else {
			log.Printf("No page context available for this connection")
		}
	}

	if useLLM && llmClient != nil && llm.ShouldUseLLM(originalGoal) {
		log.Println("Using LLM for goal parsing with page context")
		llmSequence, err := llm.ParseGoalWithLLM(llmClient, originalGoal, pageContext)
		if err != nil {
			log.Printf("LLM parsing failed: %v, falling back to rules", err)
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
	}

	commands := []CommandPayload{}

	if strings.Contains(goal, " and ") || strings.Contains(goal, ", then ") || strings.Contains(goal, " then ") {
		commands = parseMultiStepGoal(goal)
	} else {
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

func parseMultiStepGoal(goal string) []CommandPayload {
	commands := []CommandPayload{}

	parts := regexp.MustCompile(`\s+(and|then|, then|, and)\s+`).Split(goal, -1)

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		command := parseSingleCommand(part)
		if command != nil {
			commands = append(commands, *command)

			if command.Action == "input" && containsSearchKeywords(part) {
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

func parseSingleCommand(goal string) *CommandPayload {
	goal = strings.ToLower(strings.TrimSpace(goal))
	log.Printf("Parsing goal: %s", goal)

	if containsNavigationKeywords(goal) {
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	if containsContentKeywords(goal) {
		return &CommandPayload{
			Action: "get_content",
		}
	}

	if containsSearchKeywords(goal) {
		return &CommandPayload{
			Action:   "input",
			Selector: "input[name='q'], textarea[name='q'], input[type='search'], input[type='text'][name='q'], #search, [role='searchbox']",
			Text:     extractSearchTermFromGoal(goal),
		}
	}

	if containsClickKeywords(goal) {
		return &CommandPayload{
			Action:   "click",
			Selector: extractSelectorFromGoal(goal),
		}
	}

	if containsNavigationKeywords(goal) && containsSearchKeywords(goal) {
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	if containsURL(goal) {
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	return nil
}

func extractURLFromGoal(goal string) string {
	urlRegex := regexp.MustCompile(`(?i)(?:https?://)?(?:www\.)?([a-zA-Z0-9-]+\.(?:com|org|net|edu|gov|io|co))(?:/[^\s]*)?`)
	match := urlRegex.FindString(goal)
	if match != "" {
		if !strings.HasPrefix(match, "http") {
			return "https://" + match
		}
		return match
	}

	words := strings.Fields(goal)
	for _, word := range words {
		if containsURL(word) {
			if !strings.HasPrefix(word, "http") {
				return "https://" + word
			}
			return word
		}
	}

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

	return "https://google.com"
}

func extractSelectorFromGoal(goal string) string {
	if strings.Contains(goal, "button") {
		return "button"
	}
	if strings.Contains(goal, "link") {
		return "a"
	}
	return "*"
}

func extractSearchTermFromGoal(goal string) string {
	goal = strings.ToLower(goal)

	patterns := []string{"search for ", "search ", "find ", "look for "}
	for _, pattern := range patterns {
		if idx := strings.Index(goal, pattern); idx != -1 {
			term := goal[idx+len(pattern):]
			return strings.TrimSpace(term)
		}
	}

	return strings.TrimSpace(goal)
}

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
	urlPatterns := []string{".com", ".org", ".net", ".edu", ".gov", "http", "www."}
	for _, pattern := range urlPatterns {
		if strings.Contains(goal, pattern) {
			return true
		}
	}
	return false
}

func handlePageContent(conn *websocket.Conn, payload interface{}) error {
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

	pageContexts[conn] = &llm.PageContext{
		URL:         contentPayload.URL,
		Title:       contentPayload.Title,
		ContentType: determineContentTypeFromHTML(contentPayload.HTML),
		HTML:        contentPayload.HTML,
		Text:        contentPayload.Text,
	}

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

	return sendMessage(conn, &Message{
		Type:    "CONTENT_ANALYSIS",
		Payload: analysis,
	})
}

func determineContentTypeFromHTML(htmlContent string) string {
	htmlLower := strings.ToLower(htmlContent)
	if strings.Contains(htmlLower, "amazon.com") || strings.Contains(htmlLower, "field-keywords") {
		return "ecommerce"
	}
	if strings.Contains(htmlLower, "input[name='q']") || strings.Contains(htmlLower, "search") {
		return "search"
	}
	if strings.Contains(htmlLower, "form") {
		return "form"
	}
	return "general"
}

func analyzePageContent(htmlContent string) (*ContentAnalysisResult, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(htmlContent))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %v", err)
	}

	result := &ContentAnalysisResult{
		Selectors:   []string{},
		Suggestions: []string{},
	}

	doc.Find("input, button, a, select, textarea").Each(func(i int, s *goquery.Selection) {
		selector := generateSmartSelector(s)
		if selector != "" {
			result.Selectors = append(result.Selectors, selector)
		}
	})

	result.ContentType = determineContentType(doc)
	result.Suggestions = generateActionSuggestions(doc)

	return result, nil
}

func generateSmartSelector(s *goquery.Selection) string {
	if id, exists := s.Attr("id"); exists && id != "" {
		return "#" + id
	}

	if name, exists := s.Attr("name"); exists && name != "" {
		return "[name='" + name + "']"
	}

	if class, exists := s.Attr("class"); exists && class != "" {
		classes := strings.Fields(class)
		if len(classes) == 1 {
			return "." + classes[0]
		}
	}

	if role, exists := s.Attr("role"); exists && role != "" {
		return "[role='" + role + "']"
	}

	tagName := goquery.NodeName(s)
	if tagType, exists := s.Attr("type"); exists && tagType != "" {
		return tagName + "[type='" + tagType + "']"
	}

	return tagName
}

func determineContentType(doc *goquery.Document) string {
	if doc.Find("input[type='search'], input[name='q'], [role='searchbox']").Length() > 0 {
		return "search"
	}

	if doc.Find("form").Length() > 0 {
		return "form"
	}

	if doc.Find("nav, .navigation, .menu").Length() > 0 {
		return "navigation"
	}

	return "general"
}

func generateActionSuggestions(doc *goquery.Document) []string {
	var suggestions []string

	if doc.Find("input[type='search'], input[name='q']").Length() > 0 {
		suggestions = append(suggestions, "Search for something")
	}

	linkCount := doc.Find("a[href]").Length()
	if linkCount > 0 {
		suggestions = append(suggestions, fmt.Sprintf("Click on one of %d links", linkCount))
	}

	buttonCount := doc.Find("button, input[type='submit'], input[type='button']").Length()
	if buttonCount > 0 {
		suggestions = append(suggestions, fmt.Sprintf("Click on one of %d buttons", buttonCount))
	}

	return suggestions
}

func main() {
	useLLM = os.Getenv("USE_LLM") == "true" || os.Getenv("USE_LLM") == "1"
	llmModel := os.Getenv("LLM_MODEL")
	if llmModel == "" {
		llmModel = "mistral:latest"
	}

	if useLLM {
		log.Println("Initializing LLM client...")
		llmClient = llm.NewLLMClient(llmModel)

		if err := llmClient.TestConnection(); err != nil {
			log.Printf("LLM not available: %v", err)
			log.Println("Continuing with rule-based parsing only")
			log.Println("To enable LLM: Start Ollama (ollama serve) and set USE_LLM=true")
			useLLM = false
		} else {
			log.Printf("LLM enabled with model: %s", llmModel)
		}
	} else {
		log.Println("Using rule-based parsing (set USE_LLM=true to enable AI)")
	}

	flag.Parse()

	http.HandleFunc("/ws", handler)
	log.Println("Cortex Backend started on port 8080")
	log.Println("WebSocket endpoint: ws://localhost:8080/ws")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
