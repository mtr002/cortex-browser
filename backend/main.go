package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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

	// Simple rule-based command generation
	if strings.Contains(goal, "navigate") || strings.Contains(goal, "go to") {
		// Extract URL from goal
		if strings.Contains(goal, "google.com") || strings.Contains(goal, "google") {
			return &CommandPayload{
				Action: "navigate",
				URL:    "https://google.com",
			}
		} else if strings.Contains(goal, "example.com") {
			return &CommandPayload{
				Action: "navigate",
				URL:    "https://example.com",
			}
		} else if strings.Contains(goal, "github.com") {
			return &CommandPayload{
				Action: "navigate",
				URL:    "https://github.com",
			}
		}
		// Default navigation
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	if strings.Contains(goal, "click") {
		return &CommandPayload{
			Action:   "click",
			Selector: extractSelectorFromGoal(goal),
		}
	}

	if strings.Contains(goal, "search") || strings.Contains(goal, "type") {
		return &CommandPayload{
			Action:   "input",
			Selector: "input[type='search'], input[name='q'], #search",
			Text:     extractSearchTermFromGoal(goal),
		}
	}

	// Default: try to navigate if it looks like a URL
	if strings.Contains(goal, ".com") || strings.Contains(goal, ".org") || strings.Contains(goal, "http") {
		return &CommandPayload{
			Action: "navigate",
			URL:    extractURLFromGoal(goal),
		}
	}

	return nil
}

func extractURLFromGoal(goal string) string {
	// Simple URL extraction
	words := strings.Fields(goal)
	for _, word := range words {
		if strings.Contains(word, ".com") || strings.Contains(word, ".org") || strings.HasPrefix(word, "http") {
			if !strings.HasPrefix(word, "http") {
				return "https://" + word
			}
			return word
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

func main() {
	flag.Parse()

	http.HandleFunc("/ws", handler)
	log.Println("Cortex Backend started on port 8080")
	log.Println("WebSocket endpoint: ws://localhost:8080/ws")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
