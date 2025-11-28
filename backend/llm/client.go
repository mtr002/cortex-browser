package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// LLMClient handles communication with Ollama
type LLMClient struct {
	baseURL string
	model   string
	timeout time.Duration
}

// OllamaRequest represents the request to Ollama API
type OllamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
}

// OllamaResponse represents the response from Ollama API
type OllamaResponse struct {
	Model              string `json:"model"`
	CreatedAt          string `json:"created_at"`
	Response           string `json:"response"`
	Done               bool   `json:"done"`
	TotalDuration      int64  `json:"total_duration"`
	LoadDuration       int64  `json:"load_duration"`
	PromptEvalCount    int    `json:"prompt_eval_count"`
	PromptEvalDuration int64  `json:"prompt_eval_duration"`
	EvalCount          int    `json:"eval_count"`
	EvalDuration       int64  `json:"eval_duration"`
}

// NewLLMClient creates a new Ollama client
func NewLLMClient(model string) *LLMClient {
	if model == "" {
		model = "mistral:latest" // Default model
	}

	return &LLMClient{
		baseURL: "http://localhost:11434/api/generate",
		model:   model,
		timeout: 30 * time.Second,
	}
}

// Generate sends a prompt to Ollama and returns the response
func (c *LLMClient) Generate(prompt string) (string, error) {
	request := OllamaRequest{
		Model:  c.model,
		Prompt: prompt,
		Stream: false,
	}

	jsonData, err := json.Marshal(request)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %v", err)
	}

	// Create HTTP request
	req, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: c.timeout,
	}

	// Send request
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request to Ollama: %v. Make sure Ollama is running (ollama serve)", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("ollama API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	var ollamaResp OllamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %v", err)
	}

	return ollamaResp.Response, nil
}

// TestConnection tests if Ollama is running and accessible
func (c *LLMClient) TestConnection() error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		return fmt.Errorf("ollama is not running. Start it with: ollama serve")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama returned status %d", resp.StatusCode)
	}

	log.Println("Ollama connection successful")
	return nil
}
