package doormate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/llm"
)

// PageAgent is the core agent that processes user input and generates pages.
type PageAgent struct {
	llm llm.LLM
}

// NewPageAgent instantiates a new PageAgent with the provided LLM client.
func NewPageAgent(llmClient llm.LLM) *PageAgent {
	return &PageAgent{llm: llmClient}
}

// Process parses the user intent and generates a structured page schema.
func (pa *PageAgent) Process(input string, profile *UserProfile) (*IntentSession, *GeneratedPage, error) {
	sessionID := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	pageID := fmt.Sprintf("page-%d", time.Now().UnixNano())

	intent, bubbles := pa.parseIntentAndBubbles(input, profile)

	var schema PageSchema
	var err error

	if pa.llm != nil {
		schema, err = pa.generatePageWithLLM(input, intent, bubbles, profile)
		if err != nil {
			// Fallback to beautiful mock page on LLM failure
			schema = pa.generateBeautifulMockPage(input, intent, bubbles, profile)
		}
	} else {
		schema = pa.generateBeautifulMockPage(input, intent, bubbles, profile)
	}

	session := &IntentSession{
		ID:              sessionID,
		RawInput:        input,
		Intent:          intent,
		SelectedBubbles: []string{},
		Bubbles:         bubbles,
		PageIDs:         []string{pageID},
		CreatedAt:       time.Now().Unix(),
		UpdatedAt:       time.Now().Unix(),
	}

	page := &GeneratedPage{
		ID:        pageID,
		SessionID: sessionID,
		Schema:    schema,
		CreatedAt: time.Now().Unix(),
	}

	return session, page, nil
}

func (pa *PageAgent) parseIntentAndBubbles(input string, profile *UserProfile) (string, []string) {
	lower := strings.ToLower(input)
	var intent string
	var bubbles []string

	// Simple trigger checks for high reliability
	switch {
	case strings.Contains(lower, "security") || strings.Contains(lower, "lock") || strings.Contains(lower, "alarm"):
		intent = "security"
		bubbles = []string{"Biometric Lock", "Smart Deadbolt", "Door Sensors", "Video Doorbell"}
	case strings.Contains(lower, "design") || strings.Contains(lower, "color") || strings.Contains(lower, "style") || strings.Contains(lower, "look"):
		intent = "design"
		bubbles = []string{"Modern Minimalist", "Classic Wood", "Industrial Steel", "Bold Custom Color"}
	case strings.Contains(lower, "automation") || strings.Contains(lower, "smart") || strings.Contains(lower, "assistant") || strings.Contains(lower, "control"):
		intent = "automation"
		bubbles = []string{"Zigbee Controller", "Apple HomeKit", "Home Assistant", "Voice Prompts"}
	default:
		intent = "general"
		bubbles = []string{"Interactive Guides", "System Diagnostic", "Energy Efficiency", "Material Durability"}
	}

	// Mix in profile elements if any
	if profile != nil {
		for _, tag := range profile.PreferenceTags {
			if len(bubbles) < 6 {
				bubbles = append(bubbles, "Tag: "+tag)
			}
		}
	}

	return intent, bubbles
}

func (pa *PageAgent) generatePageWithLLM(input, intent string, bubbles []string, profile *UserProfile) (PageSchema, error) {
	var prefTags string
	if profile != nil {
		prefTags = strings.Join(profile.PreferenceTags, ", ")
	}

	prompt := fmt.Sprintf(`You are DoorMate, an advanced Page-First AI Assistant. Your task is to output a single, valid, raw JSON object matching the PageSchema.
DO NOT return any markdown wrapping, chat text, or explanation. Only return raw JSON.

Input: %s
Intent: %s
Top Predicted Options: %s
User Preferences: %s

The JSON structure must match this schema:
{
  "title": "Page Title",
  "summary": "Brief summary",
  "template_id": "overview" (or "recommendation", "comparison", "guide"),
  "blocks": [
    {
      "type": "overview" (or "comparison", "list", "chart", "diagram", "timeline", "cards", "gallery", "decision_tree"),
      "title": "Block Title",
      "content": "Paragraph content",
      "items": ["list item 1", "list item 2"],
      "headers": ["column 1", "column 2"],
      "rows": [["row 1 col 1", "row 1 col 2"]],
      "data_points": [{"label": "A", "value": 75}],
      "nodes": [{"id": "n1", "label": "Start", "type": "start"}],
      "edges": [{"from": "n1", "to": "n2", "label": "Yes"}]
    }
  ],
  "follow_ups": ["Next step bubble 1", "Next step bubble 2"]
}

Make sure you include:
1. An elegant title.
2. A clear summary.
3. At least one "list", "cards", or "comparison" block.
4. At least one SVG-ready structural "chart" or flowchart "diagram" block.
`, input, intent, strings.Join(bubbles, ", "), prefTags)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reply, err := pa.llm.GenerateCtx(ctx, prompt)
	if err != nil {
		return PageSchema{}, err
	}

	// Clean code blocks if returned
	reply = strings.TrimPrefix(reply, "```json")
	reply = strings.TrimPrefix(reply, "```")
	reply = strings.TrimSuffix(reply, "```")
	reply = strings.TrimSpace(reply)

	var schema PageSchema
	if err := json.Unmarshal([]byte(reply), &schema); err != nil {
		return PageSchema{}, err
	}
	return schema, nil
}

func (pa *PageAgent) generateBeautifulMockPage(input, intent string, bubbles []string, profile *UserProfile) PageSchema {
	switch intent {
	case "security":
		return PageSchema{
			Title:      "Smart Lock & Door Security Blueprint",
			Summary:    fmt.Sprintf("Your request '%s' indicates a primary focus on door security. This blueprint outlines biometric deadbolts, remote controls, and automated locking parameters.", input),
			TemplateID: "recommendation",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Advanced Security Assessment",
					Content: "Securing your physical gateway is the first line of defense. Modern biometric authentication (fingerprint + 3D facial scanning) combined with heavy-duty Grade 1 physical deadbolts prevents both manual forced entry and cyber-attacks.",
				},
				{
					Type:  "cards",
					Title: "Top-Rated Biometric Security Options",
					Items: []string{
						"Apex Bio-Lock: Fingerprint + Bluetooth, Grade 1 certified.",
						"Sentinel SmartGuard: Built-in 2K camera with on-device AI facial recognition.",
						"CipherShield Pro: PIN code with anti-peep decoy mode + physical backup key.",
					},
				},
				{
					Type:    "comparison",
					Title:   "Comparison of Secure Entrance Methods",
					Headers: []string{"Feature", "Biometric (Fingerprint)", "Pin Code", "Smart Card / NFC"},
					Rows: [][]string{
						{"Speed", "Under 0.5s", "Moderate (2-3s)", "Instant (0.1s)"},
						{"Security", "Highest (uncloneable)", "High (can be shared)", "Medium (card can be lost)"},
						{"Convenience", "No keys needed", "Needs memory", "Requires card"},
					},
				},
				{
					Type:  "chart",
					Title: "Intrusion Attempt Deterrence Rate by Feature",
					DataPoints: []ChartDataPoint{
						{Label: "Biometric Lock", Value: 98.2},
						{Label: "Video Intercom", Value: 85.5},
						{Label: "Visible Alarm Sensor", Value: 72.0},
						{Label: "Standard Key Lock", Value: 12.0},
					},
				},
				{
					Type:  "diagram",
					Title: "Smart Security Authentication & Lockdown Flow",
					Nodes: []DiagramNode{
						{ID: "start", Label: "User Approaches Door", Type: "start"},
						{ID: "scan", Label: "Scan Biometrics", Type: "action"},
						{ID: "decide", Label: "Is Authorized?", Type: "decision"},
						{ID: "unlock", Label: "Unlock & Welcome", Type: "end"},
						{ID: "alarm", Label: "Lockout & Alert App", Type: "end"},
					},
					Edges: []DiagramEdge{
						{From: "start", To: "scan"},
						{From: "scan", To: "decide"},
						{From: "decide", To: "unlock", Label: "Yes"},
						{From: "decide", To: "alarm", Label: "No (3 attempts)"},
					},
				},
			},
			FollowUps: []string{
				"What happens if internet is cut?",
				"Show lock power consumption.",
				"Integrate video doorbell flow.",
			},
		}
	case "design":
		return PageSchema{
			Title:      "Modern Gateway & Material Design Board",
			Summary:    fmt.Sprintf("Design-first options tailored for '%s'. Highlighting material selection, aesthetic integration, and architectural styles.", input),
			TemplateID: "guide",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Architectural Visual Concept",
					Content: "An entry gateway sets the aesthetic tone of the entire structure. Mixing warm, certified rustic oak with sleek, powder-coated aerospace steel creates a beautiful, mid-century modern look that is both inviting and structurally superior.",
				},
				{
					Type:  "cards",
					Title: "Aesthetic Inspiration Elements",
					Items: []string{
						"Warm Timber Finish: Sustainable red cedar.",
						"Anodized Charcoal Framework: Non-reflective matte texture.",
						"Flush Biometric Bezel: Integrated directly into the organic grain.",
					},
				},
				{
					Type:  "chart",
					Title: "Visual Preference Ranking (Architect Polls)",
					DataPoints: []ChartDataPoint{
						{Label: "Mid-Century Modern", Value: 42},
						{Label: "Industrial Minimalist", Value: 28},
						{Label: "Rustic Heritage", Value: 18},
						{Label: "Art Deco Chic", Value: 12},
					},
				},
			},
			FollowUps: []string{
				"Explore cedar wood options.",
				"What are custom colors available?",
				"Show me minimal smart bezels.",
			},
		}
	case "automation":
		return PageSchema{
			Title:      "Smart Home Gateway Automation Blueprint",
			Summary:    fmt.Sprintf("Automation options optimized for '%s'. Showcasing protocol integration, local control latency, and event-driven triggers.", input),
			TemplateID: "recommendation",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Home Automation Architecture",
					Content: "Integrating your gateway with smart home ecosystems enables seamless entry. By utilizing local control protocols like Zigbee 3.0 or Thread, you minimize latency and ensure your door remains fully controllable even during internet outages.",
				},
				{
					Type:  "cards",
					Title: "Smart Controller Protocols",
					Items: []string{
						"Zigbee 3.0 Hub: Low power, high reliability mesh network.",
						"Apple HomeKit & Thread: Ultra-fast local control and native iOS integration.",
						"Home Assistant Integration: Complete local control, open-source flexibility.",
					},
				},
				{
					Type:  "chart",
					Title: "Response Latency by Protocol (ms)",
					DataPoints: []ChartDataPoint{
						{Label: "Local Zigbee", Value: 15.0},
						{Label: "Local Wi-Fi", Value: 45.0},
						{Label: "Cloud API", Value: 250.0},
					},
				},
				{
					Type:  "diagram",
					Title: "Smart Automation Trigger & Action Flow",
					Nodes: []DiagramNode{
						{ID: "start", Label: "User Approaches", Type: "start"},
						{ID: "detect", Label: "Motion Sensor", Type: "action"},
						{ID: "decide", Label: "Is Night?", Type: "decision"},
						{ID: "light", Label: "Turn on Light", Type: "end"},
					},
					Edges: []DiagramEdge{
						{From: "start", To: "detect"},
						{From: "detect", To: "decide"},
						{From: "decide", To: "light", Label: "Yes"},
					},
				},
			},
			FollowUps: []string{
				"Configure Zigbee network.",
				"How to set up Home Assistant?",
				"Show voice prompt options.",
			},
		}
	default:
		return PageSchema{
			Title:      "Personalized Gateway Consultation Overview",
			Summary:    fmt.Sprintf("Welcome to DoorMate. Custom analysis generated for your query: '%s'.", input),
			TemplateID: "overview",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Seamless AI Navigation Layer",
					Content: "DoorMate operates as a page-first agent. It predicts your requirements and designs interactive schemas rather than engaging in verbose chat, helping you configure, visualize, and command your Smart Gateway quickly.",
				},
				{
					Type:  "list",
					Title: "Your Gateway Options Roadmap",
					Items: []string{
						"Analyze structural material parameters and environmental resistance.",
						"Configure real-time monitoring and event-driven camera feeds.",
						"Design custom smart alerts and profile-based guest keys.",
					},
				},
			},
			FollowUps: []string{
				"Configure door security locks.",
				"Browse visual material styles.",
				"How does dynamic profile learning work?",
			},
		}
	}
}
