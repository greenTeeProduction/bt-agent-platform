package benchmark

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// ToolBenchStep represents a single step in a multi-API workflow.
type ToolBenchStep struct {
	Action     string            `json:"action"`
	API        string            `json:"api"`
	Parameters map[string]string `json:"parameters"`
}

// ToolBenchParam describes a single parameter for an API.
type ToolBenchParam struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ToolBenchAPI represents an available API the agent can choose from.
type ToolBenchAPI struct {
	Name        string           `json:"name"`
	Endpoint    string           `json:"endpoint"`
	Method      string           `json:"method"`
	Description string           `json:"description"`
	Parameters  []ToolBenchParam `json:"parameters"`
}

// ToolBenchEntry mirrors a ToolBench test case: a user task with required APIs,
// available distractors, and a sequence of steps.
type ToolBenchEntry struct {
	ID              string          `json:"id"`
	Category        string          `json:"category"`
	TaskDescription string          `json:"task_description"`
	RequiredAPIs    []string        `json:"required_apis"`
	Steps           []ToolBenchStep `json:"steps"`
	AvailableAPIs   []ToolBenchAPI  `json:"available_apis"`
}

// ToolBenchMetrics holds aggregate ToolBench evaluation results.
type ToolBenchMetrics struct {
	TotalTasks           int     `json:"total_tasks"`
	APISelectionAccuracy float64 `json:"api_selection_accuracy"` // fraction of tasks with correct API selection
	StepCompletionRate   float64 `json:"step_completion_rate"`   // fraction of steps that completed
	SuccessRate          float64 `json:"success_rate"`           // fraction of tasks that fully succeeded
}

// EvaluateToolBench tests whether the tree correctly selects which APIs/tools
// to invoke for a set of ToolBench entries.
func EvaluateToolBench(tree *evolution.SerializableNode, entries []ToolBenchEntry, llm llm.LLM) *ToolBenchMetrics {
	var totalTasks int
	correctAPISelection := 0
	totalSteps := 0
	completedSteps := 0
	totalSuccesses := 0

	for _, entry := range entries {
		totalTasks++

		// Build a task description that includes available APIs so the tree
		// can "see" the options via the normal engine path-detection mechanism.
		task := fmt.Sprintf("Task: %s\n\nAvailable APIs: %s",
			entry.TaskDescription,
			formatAvailableAPIs(entry.AvailableAPIs))

		bb := &engine.Blackboard{
			Task: task,
			LLM:  llm,
		}
		bt := engine.BuildTree(tree, bb)
		output := engine.RunTask(bb, bt)

		path := detectPath(output, bb)

		// Check API selection accuracy: does the detected path match any required API?
		apiMatch := false
		for _, api := range entry.RequiredAPIs {
			if strings.Contains(strings.ToLower(path), strings.ToLower(api)) ||
				strings.Contains(strings.ToLower(output), strings.ToLower(api)) {
				apiMatch = true
				break
			}
		}
		if apiMatch {
			correctAPISelection++
		}

		// Step completion: count steps that produced output
		for _, step := range entry.Steps {
			totalSteps++
			if strings.Contains(strings.ToLower(output), strings.ToLower(step.API)) {
				completedSteps++
			}
		}

		if bb.Outcome == "success" {
			totalSuccesses++
		}
	}

	n := totalTasks
	if n == 0 {
		return &ToolBenchMetrics{}
	}

	apiAcc := 0.0
	if n > 0 {
		apiAcc = float64(correctAPISelection) / float64(n)
	}
	stepRate := 0.0
	if totalSteps > 0 {
		stepRate = float64(completedSteps) / float64(totalSteps)
	}
	successRate := float64(totalSuccesses) / float64(n)

	return &ToolBenchMetrics{
		TotalTasks:           n,
		APISelectionAccuracy: apiAcc,
		StepCompletionRate:   stepRate,
		SuccessRate:          successRate,
	}
}

func formatAvailableAPIs(apis []ToolBenchAPI) string {
	var sb strings.Builder
	for i, api := range apis {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(api.Name)
		sb.WriteString(" (")
		sb.WriteString(api.Method)
		sb.WriteString(" ")
		sb.WriteString(api.Endpoint)
		sb.WriteString(")")
	}
	return sb.String()
}

// BuiltinToolBench returns 15 representative ToolBench entries covering
// the major API categories: weather, translation, search, calculator,
// database, file ops, email, calendar, maps, finance, code execution,
// text analysis, image, audio, and messaging.
func BuiltinToolBench() []ToolBenchEntry {
	return []ToolBenchEntry{
		// 1. Weather
		{
			ID:              "tb-weather-001",
			Category:        "weather",
			TaskDescription: "What is the weather forecast for Tokyo tomorrow?",
			RequiredAPIs:    []string{"GetWeather"},
			Steps: []ToolBenchStep{
				{Action: "call_weather_api", API: "GetWeather", Parameters: map[string]string{"city": "Tokyo", "days": "1"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "GetWeather", Endpoint: "/weather/forecast", Method: "GET", Description: "Get weather forecast for a city", Parameters: []ToolBenchParam{
					{Name: "city", Type: "string", Description: "City name", Required: true},
					{Name: "days", Type: "integer", Description: "Forecast days", Required: false},
				}},
				{Name: "GetCurrentTime", Endpoint: "/time/current", Method: "GET", Description: "Get current time for a timezone", Parameters: []ToolBenchParam{
					{Name: "timezone", Type: "string", Description: "IANA timezone", Required: true},
				}},
			},
		},
		// 2. Translation
		{
			ID:              "tb-translation-001",
			Category:        "translation",
			TaskDescription: "Translate 'Hello, how are you?' from English to French.",
			RequiredAPIs:    []string{"TranslateText"},
			Steps: []ToolBenchStep{
				{Action: "call_translate", API: "TranslateText", Parameters: map[string]string{"text": "Hello, how are you?", "source": "en", "target": "fr"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "TranslateText", Endpoint: "/translate", Method: "POST", Description: "Translate text between languages", Parameters: []ToolBenchParam{
					{Name: "text", Type: "string", Description: "Text to translate", Required: true},
					{Name: "source", Type: "string", Description: "Source language code", Required: true},
					{Name: "target", Type: "string", Description: "Target language code", Required: true},
				}},
				{Name: "DetectLanguage", Endpoint: "/detect", Method: "POST", Description: "Detect language of text", Parameters: []ToolBenchParam{
					{Name: "text", Type: "string", Description: "Text to analyze", Required: true},
				}},
			},
		},
		// 3. Search
		{
			ID:              "tb-search-001",
			Category:        "search",
			TaskDescription: "Search for the latest news about quantum computing breakthroughs.",
			RequiredAPIs:    []string{"WebSearch"},
			Steps: []ToolBenchStep{
				{Action: "search_web", API: "WebSearch", Parameters: map[string]string{"query": "quantum computing breakthroughs 2025", "limit": "10"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "WebSearch", Endpoint: "/search/web", Method: "GET", Description: "Search the web for content", Parameters: []ToolBenchParam{
					{Name: "query", Type: "string", Description: "Search query", Required: true},
					{Name: "limit", Type: "integer", Description: "Max results", Required: false},
				}},
				{Name: "GetStockPrice", Endpoint: "/finance/stock", Method: "GET", Description: "Get current stock price", Parameters: []ToolBenchParam{
					{Name: "symbol", Type: "string", Description: "Stock symbol", Required: true},
				}},
			},
		},
		// 4. Calculator
		{
			ID:              "tb-calculator-001",
			Category:        "calculator",
			TaskDescription: "Calculate the compound interest on $10,000 at 5% annually for 10 years.",
			RequiredAPIs:    []string{"Calculate"},
			Steps: []ToolBenchStep{
				{Action: "run_calculation", API: "Calculate", Parameters: map[string]string{"expression": "10000 * (1 + 0.05)^10"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "Calculate", Endpoint: "/math/calculate", Method: "POST", Description: "Evaluate a mathematical expression", Parameters: []ToolBenchParam{
					{Name: "expression", Type: "string", Description: "Math expression", Required: true},
				}},
				{Name: "ConvertUnits", Endpoint: "/convert", Method: "POST", Description: "Convert between units", Parameters: []ToolBenchParam{
					{Name: "value", Type: "number", Description: "Value to convert", Required: true},
					{Name: "from", Type: "string", Description: "Source unit", Required: true},
					{Name: "to", Type: "string", Description: "Target unit", Required: true},
				}},
			},
		},
		// 5. Database
		{
			ID:              "tb-database-001",
			Category:        "database",
			TaskDescription: "Find all customers who placed orders over $500 in the last 30 days.",
			RequiredAPIs:    []string{"QueryDatabase"},
			Steps: []ToolBenchStep{
				{Action: "query_db", API: "QueryDatabase", Parameters: map[string]string{"sql": "SELECT * FROM customers WHERE order_total > 500 AND order_date > NOW() - INTERVAL '30 days'"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "QueryDatabase", Endpoint: "/db/query", Method: "POST", Description: "Execute a SQL query", Parameters: []ToolBenchParam{
					{Name: "sql", Type: "string", Description: "SQL query string", Required: true},
				}},
				{Name: "InsertRecord", Endpoint: "/db/insert", Method: "POST", Description: "Insert a new record", Parameters: []ToolBenchParam{
					{Name: "table", Type: "string", Description: "Table name", Required: true},
					{Name: "data", Type: "object", Description: "Record data", Required: true},
				}},
			},
		},
		// 6. File Operations
		{
			ID:              "tb-fileops-001",
			Category:        "file_ops",
			TaskDescription: "Read the contents of /home/user/report.md and save a summary to /home/user/summary.txt.",
			RequiredAPIs:    []string{"ReadFile", "WriteFile"},
			Steps: []ToolBenchStep{
				{Action: "read_file", API: "ReadFile", Parameters: map[string]string{"path": "/home/user/report.md"}},
				{Action: "write_file", API: "WriteFile", Parameters: map[string]string{"path": "/home/user/summary.txt", "content": "Summary of report..."}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "ReadFile", Endpoint: "/fs/read", Method: "GET", Description: "Read file contents", Parameters: []ToolBenchParam{
					{Name: "path", Type: "string", Description: "File path", Required: true},
				}},
				{Name: "WriteFile", Endpoint: "/fs/write", Method: "POST", Description: "Write content to file", Parameters: []ToolBenchParam{
					{Name: "path", Type: "string", Description: "File path", Required: true},
					{Name: "content", Type: "string", Description: "File content", Required: true},
				}},
				{Name: "DeleteFile", Endpoint: "/fs/delete", Method: "DELETE", Description: "Delete a file", Parameters: []ToolBenchParam{
					{Name: "path", Type: "string", Description: "File path", Required: true},
				}},
			},
		},
		// 7. Email
		{
			ID:              "tb-email-001",
			Category:        "email",
			TaskDescription: "Send a meeting reminder email to team@example.com with subject 'Meeting Tomorrow at 10am'.",
			RequiredAPIs:    []string{"SendEmail"},
			Steps: []ToolBenchStep{
				{Action: "send_email", API: "SendEmail", Parameters: map[string]string{"to": "team@example.com", "subject": "Meeting Tomorrow at 10am", "body": "Reminder: team meeting tomorrow at 10am in Room 3."}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "SendEmail", Endpoint: "/email/send", Method: "POST", Description: "Send an email", Parameters: []ToolBenchParam{
					{Name: "to", Type: "string", Description: "Recipient email", Required: true},
					{Name: "subject", Type: "string", Description: "Email subject", Required: true},
					{Name: "body", Type: "string", Description: "Email body", Required: true},
				}},
				{Name: "CheckInbox", Endpoint: "/email/inbox", Method: "GET", Description: "Check email inbox", Parameters: []ToolBenchParam{
					{Name: "limit", Type: "integer", Description: "Max messages", Required: false},
				}},
			},
		},
		// 8. Calendar
		{
			ID:              "tb-calendar-001",
			Category:        "calendar",
			TaskDescription: "Schedule a team standup meeting for next Monday at 9am, duration 30 minutes.",
			RequiredAPIs:    []string{"CreateEvent"},
			Steps: []ToolBenchStep{
				{Action: "create_calendar_event", API: "CreateEvent", Parameters: map[string]string{"title": "Team Standup", "date": "next Monday", "time": "09:00", "duration": "30"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "CreateEvent", Endpoint: "/calendar/events", Method: "POST", Description: "Create a calendar event", Parameters: []ToolBenchParam{
					{Name: "title", Type: "string", Description: "Event title", Required: true},
					{Name: "date", Type: "string", Description: "Event date", Required: true},
					{Name: "time", Type: "string", Description: "Event time", Required: true},
					{Name: "duration", Type: "integer", Description: "Duration in minutes", Required: false},
				}},
				{Name: "ListEvents", Endpoint: "/calendar/events", Method: "GET", Description: "List calendar events", Parameters: []ToolBenchParam{
					{Name: "date", Type: "string", Description: "Date to query", Required: false},
				}},
			},
		},
		// 9. Maps
		{
			ID:              "tb-maps-001",
			Category:        "maps",
			TaskDescription: "Find the fastest driving route from San Francisco to Los Angeles.",
			RequiredAPIs:    []string{"GetDirections"},
			Steps: []ToolBenchStep{
				{Action: "get_directions", API: "GetDirections", Parameters: map[string]string{"origin": "San Francisco, CA", "destination": "Los Angeles, CA", "mode": "driving"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "GetDirections", Endpoint: "/maps/directions", Method: "GET", Description: "Get directions between two points", Parameters: []ToolBenchParam{
					{Name: "origin", Type: "string", Description: "Starting point", Required: true},
					{Name: "destination", Type: "string", Description: "Ending point", Required: true},
					{Name: "mode", Type: "string", Description: "Travel mode", Required: false},
				}},
				{Name: "Geocode", Endpoint: "/maps/geocode", Method: "GET", Description: "Convert address to coordinates", Parameters: []ToolBenchParam{
					{Name: "address", Type: "string", Description: "Address to geocode", Required: true},
				}},
			},
		},
		// 10. Finance
		{
			ID:              "tb-finance-001",
			Category:        "finance",
			TaskDescription: "Get the current stock price and P/E ratio for AAPL.",
			RequiredAPIs:    []string{"GetStockData"},
			Steps: []ToolBenchStep{
				{Action: "fetch_stock_data", API: "GetStockData", Parameters: map[string]string{"symbol": "AAPL", "metrics": "price,pe_ratio"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "GetStockData", Endpoint: "/finance/stock", Method: "GET", Description: "Get stock data and metrics", Parameters: []ToolBenchParam{
					{Name: "symbol", Type: "string", Description: "Stock symbol", Required: true},
					{Name: "metrics", Type: "string", Description: "Comma-separated metrics", Required: false},
				}},
				{Name: "GetCryptoPrice", Endpoint: "/finance/crypto", Method: "GET", Description: "Get cryptocurrency price", Parameters: []ToolBenchParam{
					{Name: "symbol", Type: "string", Description: "Crypto symbol", Required: true},
				}},
			},
		},
		// 11. Code Execution
		{
			ID:              "tb-codeexec-001",
			Category:        "code_execution",
			TaskDescription: "Run a Python script that calculates the first 20 Fibonacci numbers and prints the list.",
			RequiredAPIs:    []string{"ExecuteCode"},
			Steps: []ToolBenchStep{
				{Action: "execute_code", API: "ExecuteCode", Parameters: map[string]string{"language": "python", "code": "fib = [0, 1]\nfor i in range(2, 20):\n    fib.append(fib[-1] + fib[-2])\nprint(fib)"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "ExecuteCode", Endpoint: "/code/execute", Method: "POST", Description: "Execute code in a sandbox", Parameters: []ToolBenchParam{
					{Name: "language", Type: "string", Description: "Programming language", Required: true},
					{Name: "code", Type: "string", Description: "Source code to execute", Required: true},
				}},
				{Name: "LintCode", Endpoint: "/code/lint", Method: "POST", Description: "Lint and analyze code", Parameters: []ToolBenchParam{
					{Name: "language", Type: "string", Description: "Programming language", Required: true},
					{Name: "code", Type: "string", Description: "Source code to lint", Required: true},
				}},
			},
		},
		// 12. Text Analysis
		{
			ID:              "tb-textanalysis-001",
			Category:        "text_analysis",
			TaskDescription: "Analyze the sentiment of this customer review: 'The product is amazing, but shipping was slow.'",
			RequiredAPIs:    []string{"AnalyzeSentiment"},
			Steps: []ToolBenchStep{
				{Action: "analyze_sentiment", API: "AnalyzeSentiment", Parameters: map[string]string{"text": "The product is amazing, but shipping was slow."}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "AnalyzeSentiment", Endpoint: "/nlp/sentiment", Method: "POST", Description: "Analyze sentiment of text", Parameters: []ToolBenchParam{
					{Name: "text", Type: "string", Description: "Text to analyze", Required: true},
				}},
				{Name: "SummarizeText", Endpoint: "/nlp/summarize", Method: "POST", Description: "Summarize long text", Parameters: []ToolBenchParam{
					{Name: "text", Type: "string", Description: "Text to summarize", Required: true},
					{Name: "max_length", Type: "integer", Description: "Max summary length", Required: false},
				}},
			},
		},
		// 13. Image
		{
			ID:              "tb-image-001",
			Category:        "image",
			TaskDescription: "Resize the image at /photos/vacation.jpg to 800x600 pixels and save as thumbnail.",
			RequiredAPIs:    []string{"ResizeImage"},
			Steps: []ToolBenchStep{
				{Action: "resize_image", API: "ResizeImage", Parameters: map[string]string{"source": "/photos/vacation.jpg", "width": "800", "height": "600", "output": "/photos/vacation_thumb.jpg"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "ResizeImage", Endpoint: "/image/resize", Method: "POST", Description: "Resize an image", Parameters: []ToolBenchParam{
					{Name: "source", Type: "string", Description: "Source path", Required: true},
					{Name: "width", Type: "integer", Description: "Target width", Required: true},
					{Name: "height", Type: "integer", Description: "Target height", Required: true},
					{Name: "output", Type: "string", Description: "Output path", Required: false},
				}},
				{Name: "OCRImage", Endpoint: "/image/ocr", Method: "POST", Description: "Extract text from image", Parameters: []ToolBenchParam{
					{Name: "source", Type: "string", Description: "Image path", Required: true},
				}},
			},
		},
		// 14. Audio
		{
			ID:              "tb-audio-001",
			Category:        "audio",
			TaskDescription: "Transcribe the meeting recording at /recordings/standup.mp3 to English text.",
			RequiredAPIs:    []string{"TranscribeAudio"},
			Steps: []ToolBenchStep{
				{Action: "transcribe", API: "TranscribeAudio", Parameters: map[string]string{"source": "/recordings/standup.mp3", "language": "en", "format": "text"}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "TranscribeAudio", Endpoint: "/audio/transcribe", Method: "POST", Description: "Transcribe audio to text", Parameters: []ToolBenchParam{
					{Name: "source", Type: "string", Description: "Audio file path", Required: true},
					{Name: "language", Type: "string", Description: "Language code", Required: true},
					{Name: "format", Type: "string", Description: "Output format", Required: false},
				}},
				{Name: "TextToSpeech", Endpoint: "/audio/tts", Method: "POST", Description: "Convert text to speech", Parameters: []ToolBenchParam{
					{Name: "text", Type: "string", Description: "Text to speak", Required: true},
					{Name: "voice", Type: "string", Description: "Voice ID", Required: false},
				}},
			},
		},
		// 15. Messaging
		{
			ID:              "tb-messaging-001",
			Category:        "messaging",
			TaskDescription: "Send a Slack message to the #general channel: 'Deploy v2.3.1 completed successfully.'",
			RequiredAPIs:    []string{"SendMessage"},
			Steps: []ToolBenchStep{
				{Action: "send_message", API: "SendMessage", Parameters: map[string]string{"platform": "slack", "channel": "#general", "text": "Deploy v2.3.1 completed successfully."}},
			},
			AvailableAPIs: []ToolBenchAPI{
				{Name: "SendMessage", Endpoint: "/messaging/send", Method: "POST", Description: "Send a message to a chat platform", Parameters: []ToolBenchParam{
					{Name: "platform", Type: "string", Description: "Platform (slack, teams, discord)", Required: true},
					{Name: "channel", Type: "string", Description: "Channel or user ID", Required: true},
					{Name: "text", Type: "string", Description: "Message text", Required: true},
				}},
				{Name: "ListChannels", Endpoint: "/messaging/channels", Method: "GET", Description: "List available channels", Parameters: []ToolBenchParam{
					{Name: "platform", Type: "string", Description: "Chat platform", Required: true},
				}},
			},
		},
	}
}
