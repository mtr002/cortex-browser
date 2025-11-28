# LLM Integration Setup Guide

## Quick Start

### 1. Make sure Ollama is running

```bash
# Check if Ollama is running
ollama list

# If not running, start it:
ollama serve
```

### 2. Enable LLM in CortexBrowser

```bash
# Set environment variable
export USE_LLM=true
export LLM_MODEL=mistral:latest  # Optional, defaults to mistral:latest

# Run the backend
cd cortex-browser/backend
go run main.go
```

You should see:
```
🤖 Initializing LLM client...
✅ Ollama connection successful
✅ LLM enabled with model: mistral:latest
```

### 3. Test It!

Try these goals in the extension:
- **"Find information about neural networks"** (LLM will understand this as "search for neural networks")
- **"Get the latest news"** (LLM will interpret and plan steps)
- **"Navigate to google.com and search for AI"** (Will use rules - simple pattern)

## How It Works

### Hybrid System
- **Simple goals** → Uses fast rule-based parsing
- **Complex/ambiguous goals** → Uses LLM for intelligent understanding
- **LLM fails** → Automatically falls back to rules

### When LLM is Used
The system uses LLM for goals that:
- Contain ambiguous words: "find", "get", "show", "look for"
- Are longer than 80 characters
- Don't match simple patterns like "navigate to" or "search for"

### Example Goals

**Will use LLM:**
- "Find information about AI"
- "Get the latest news about technology"
- "Show me how to learn programming"

**Will use rules (faster):**
- "Navigate to google.com"
- "Search for machine learning"
- "Go to github.com and search for golang"

## Troubleshooting

### LLM not working?
1. Make sure Ollama is running: `ollama serve`
2. Check if model is available: `ollama list`
3. Test connection: The backend will show an error if Ollama isn't accessible

### Want to disable LLM?
Just don't set `USE_LLM=true` - the system will use rule-based parsing only.

## Free and Local!

✅ **Ollama is completely free**
✅ **Runs locally on your machine**
✅ **No API keys needed**
✅ **No rate limits**
✅ **Privacy - your data stays local**

Enjoy intelligent goal parsing! 🚀
