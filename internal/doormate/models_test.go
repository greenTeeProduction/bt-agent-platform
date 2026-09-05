package doormate

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestIntentSessionSerialization(t *testing.T) {
	session := IntentSession{
		ID:              "sess-123",
		RawInput:        "setup smart lock",
		Intent:          "configure_lock",
		SelectedBubbles: []string{"smart lock"},
		Bubbles:         []string{"smart lock", "zigbee", "keyless"},
		PageIDs:         []string{"page-123"},
		CreatedAt:       1780651400,
		UpdatedAt:       1780651500,
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("failed to marshal IntentSession: %v", err)
	}

	var deserialized IntentSession
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal IntentSession: %v", err)
	}

	if !reflect.DeepEqual(session, deserialized) {
		t.Errorf("IntentSession mismatch.\nExpected: %+v\nGot:      %+v", session, deserialized)
	}
}

func TestBlockSerialization(t *testing.T) {
	block := Block{
		Type:    "comparison",
		Title:   "Secure Entry Methods",
		Content: "A detailed comparison of biometric options.",
		Items:   []string{"Biometric", "PIN Code"},
		Headers: []string{"Feature", "Biometric"},
		Rows: [][]string{
			{"Speed", "Under 0.5s"},
		},
		DataPoints: []ChartDataPoint{
			{Label: "Biometric Lock", Value: 98.2},
		},
		Nodes: []DiagramNode{
			{ID: "start", Label: "User Approaches", Type: "start"},
		},
		Edges: []DiagramEdge{
			{From: "start", To: "scan", Label: "Scan"},
		},
	}

	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("failed to marshal Block: %v", err)
	}

	var deserialized Block
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal Block: %v", err)
	}

	if !reflect.DeepEqual(block, deserialized) {
		t.Errorf("Block mismatch.\nExpected: %+v\nGot:      %+v", block, deserialized)
	}
}

func TestPageSchemaSerialization(t *testing.T) {
	schema := PageSchema{
		Title:      "Smart Lock & Door Security Blueprint",
		Summary:    "Door security overview.",
		TemplateID: "recommendation",
		Blocks: []Block{
			{
				Type:    "overview",
				Title:   "Advanced Security Assessment",
				Content: "Securing your physical gateway is the first line of defense.",
			},
		},
		FollowUps: []string{"Show lock power consumption."},
	}

	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("failed to marshal PageSchema: %v", err)
	}

	var deserialized PageSchema
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal PageSchema: %v", err)
	}

	if !reflect.DeepEqual(schema, deserialized) {
		t.Errorf("PageSchema mismatch.\nExpected: %+v\nGot:      %+v", schema, deserialized)
	}
}

func TestGeneratedPageSerialization(t *testing.T) {
	page := GeneratedPage{
		ID:         "page-123",
		SessionID:  "sess-123",
		Bookmarked: true,
		Rating:     5,
		CreatedAt:  1780651400,
		Schema: PageSchema{
			Title:      "Security Blueprint",
			Summary:    "Summary description",
			TemplateID: "overview",
			Blocks: []Block{
				{Type: "list", Items: []string{"Item 1", "Item 2"}},
			},
			FollowUps: []string{"Follow up 1"},
		},
	}

	data, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("failed to marshal GeneratedPage: %v", err)
	}

	var deserialized GeneratedPage
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal GeneratedPage: %v", err)
	}

	if !reflect.DeepEqual(page, deserialized) {
		t.Errorf("GeneratedPage mismatch.\nExpected: %+v\nGot:      %+v", page, deserialized)
	}
}

func TestUserProfileSerialization(t *testing.T) {
	profile := UserProfile{
		ID:             "user-123",
		PreferenceTags: []string{"smart", "security"},
		BookmarkIDs:    []string{"page-123"},
		PreferredStyle: "visual",
		UpdatedAt:      1780651400,
	}

	data, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("failed to marshal UserProfile: %v", err)
	}

	var deserialized UserProfile
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal UserProfile: %v", err)
	}

	if !reflect.DeepEqual(profile, deserialized) {
		t.Errorf("UserProfile mismatch.\nExpected: %+v\nGot:      %+v", profile, deserialized)
	}
}

func TestFeedbackEventSerialization(t *testing.T) {
	event := FeedbackEvent{
		ID:        "feed-123",
		SessionID: "sess-123",
		PageID:    "page-123",
		Type:      "bookmark",
		Value:     "page-123",
		Timestamp: 1780651400,
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal FeedbackEvent: %v", err)
	}

	var deserialized FeedbackEvent
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal FeedbackEvent: %v", err)
	}

	if !reflect.DeepEqual(event, deserialized) {
		t.Errorf("FeedbackEvent mismatch.\nExpected: %+v\nGot:      %+v", event, deserialized)
	}
}
