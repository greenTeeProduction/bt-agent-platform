# DoorMate Page-First AI Assistant Design

## Summary

DoorMate is a web-based adaptive AI assistant that replaces chat replies with generated web pages. The user starts from a phrase, image, or voice input; DoorMate predicts intent through bubble buttons; the user refines with subbubbles; then a WhatsApp-style send bubble asks the agent to generate an interactive response page.

The selected product direction is a general assistant, not a door or home design app. The “DoorMate” name represents a doorway into the right generated page, workflow, or answer.

## Product Positioning

DoorMate is a Page-First OS for AI interaction:

- No traditional chat thread as the primary experience.
- Intent is selected through predicted bubbles and subbubbles.
- Every agent response is rendered as a web page or mini-site.
- Every generated page includes follow-up interaction bubbles.
- Bookmarks, ratings, and feedback events improve future suggestions.
- Voice can be an early input adapter; full video interaction is a later phase.

## Core User Journey

1. The user enters a phrase, speaks, or provides an image.
2. DoorMate parses the input into a structured intent.
3. DoorMate predicts likely bubbles based on the current intent and user profile.
4. The user selects one or more bubbles; selected bubbles can open subbubbles.
5. The user taps a circular send bubble to confirm the intent bundle.
6. The agent generates a structured response page.
7. The page ends with follow-up bubbles such as refine, compare, bookmark, rate, share, or create another page.

## MVP Scope

The MVP proves the smallest complete DoorMate loop:

- Intent Canvas with phrase input, picture-style background, selected intent chips, and send bubble.
- Bubble and Subbubble Engine with predicted top-level choices and contextual refinements.
- Structured Page Generation where the agent returns a JSON page schema, not plain chat text.
- Generated Page Renderer for hero content, sections, cards, comparisons, steps, diagrams, charts, lists, and follow-up bubbles.
- Reusable Page Template Library so the agent assembles responses from approved layouts instead of inventing every page from scratch.
- Bookmarking, rating, and lightweight profile learning.
- Persistent core entities: Intent Session, Generated Page, User Profile, and Feedback Event.

## Out of Scope for MVP

- Full video understanding or live video conversation.
- Autonomous multi-step workflow execution.
- Public marketplace or sharing network for generated pages.
- Complex team permissions.
- Deep implicit long-term memory beyond explicit bookmarks, ratings, preference tags, and feedback events.

## UX Architecture

### Intent Canvas

The home screen is an adaptive input canvas. It shows a picture phrase background, a phrase or voice input, predicted intent bubbles, selected intent chips, and a floating send bubble.

The canvas should feel closer to choosing and shaping intent than typing into a chatbot.

### Bubble System

Bubbles are predicted actions, categories, tones, constraints, or next steps. Selecting a bubble can open subbubbles with more specific options. The selected intent bundle is visible before sending.

### Send Bubble

The send bubble is a circular floating action button inspired by WhatsApp. It confirms the current intent bundle and requests a generated page.

### Generated Page

The response is a structured web page. It can include:

- Title and hero summary.
- Visual or media block.
- Explanatory sections.
- Template-based blocks such as cards, comparisons, steps, lists, charts, diagrams, timelines, galleries, or recommendations.
- Follow-up bubbles.
- Bookmark and rating controls.

Generated pages should reuse approved templates whenever possible. The agent chooses the right template for the intent, fills it with structured content, and only requests a new template when the existing library cannot represent the answer well.

### Follow-Up Bubbles

Every generated page must end with follow-up bubbles. This is a hard UX rule. Follow-ups keep the interaction moving without returning the user to a blank prompt.

Examples:

- Refine this.
- Compare options.
- Make it shorter.
- Create a shopping page.
- Add a voice explanation.
- Save to profile.
- More like this.
- Less like this.

## Technical Architecture

DoorMate has five main modules:

1. Web UI
   - Intent Canvas.
   - Bubble selection state.
   - Send bubble.
   - Generated Page Renderer.
   - Bookmark and rating controls.

2. Intent Parser
   - Accepts raw phrase, voice transcript, image metadata, and selected bubbles.
   - Produces a structured intent object.

3. Bubble Engine
   - Returns ranked top-level bubbles.
   - Returns subbubbles for selected bubbles.
   - Uses current intent, profile tags, and recent feedback.

4. Page Agent
   - Generates a structured page schema.
   - May reason internally with an LLM, but must return constrained JSON for rendering.
   - Selects reusable templates for charts, diagrams, lists, comparison grids, timelines, cards, and other common response types.

5. Page Template Library
   - Defines reusable response templates and block templates.
   - Provides rendering contracts for common layouts such as chart, diagram, list, table, gallery, step-by-step guide, comparison, and decision tree.
   - Keeps visual output consistent, accessible, and safe to render.

6. Profile and Feedback Store
   - Stores user profile tags, bookmarks, ratings, selected bubbles, generated page references, and feedback events.
   - Provides explicit, editable personalization.

## Data Model

### Intent Session

Stores the current interaction state:

- Raw input.
- Parsed intent.
- Selected bubbles.
- Generated page IDs.
- Follow-up actions.
- Timestamps.

### Generated Page

Stores the rendered response:

- Page ID.
- Intent session ID.
- Page schema.
- Template IDs used by the page and its blocks.
- Follow-up bubbles.
- Bookmark state.
- Rating.
- Created timestamp.

### User Profile

Stores user-level personalization:

- Preference tags.
- Bookmarked page IDs.
- Rating history.
- Preferred interaction style.
- Privacy and memory settings.

### Feedback Event

Stores behavior signals:

- Bubble clicks.
- Subbubble selections.
- Bookmark events.
- Ratings.
- Follow-up clicks.
- “More like this” and “less like this” signals.

## Page Schema

The agent should return a structured page response rather than freeform chat text. A first schema can include:

- `title`
- `summary`
- `hero`
- `templateId`
- `sections`
- `blocks`
- `cards`
- `comparisons`
- `steps`
- `lists`
- `charts`
- `diagrams`
- `media`
- `followUps`
- `profileSignals`

The schema should be strict enough for safe rendering and flexible enough to support multiple response types.

## Template Library

DoorMate should maintain a reusable page and block template library. The agent should not freely generate arbitrary HTML as its default behavior. Instead, it should select from known templates, populate structured slots, and ask for a new template only when necessary.

Initial template types:

- Overview page.
- Recommendation page.
- Comparison page.
- Step-by-step guide.
- Chart block.
- Diagram block.
- Timeline block.
- List block.
- Table block.
- Card grid.
- Gallery.
- Decision tree.
- Follow-up action rail.

Each template should define:

- Required input fields.
- Optional fields.
- Rendering rules.
- Accessibility requirements.
- Supported follow-up bubble types.
- Feedback signals the template can emit.

## Phase Plan

### Phase 1: Static Prototype

Build the front-end interaction with mock data:

- Intent Canvas.
- Bubble and subbubble selection.
- Send bubble.
- Generated Page Renderer.
- Sample page schemas.
- Initial reusable templates for lists, cards, comparisons, charts, diagrams, timelines, and follow-up rails.

### Phase 2: AI Loop

Connect AI services:

- Intent parsing.
- Bubble prediction.
- Structured page generation.
- Template selection.
- Basic prompt and schema validation.

### Phase 3: Memory Loop

Add persistence and feedback:

- Bookmarks.
- Ratings.
- User profile tags.
- Feedback events.
- Reopen saved pages.

### Phase 4: Media and Polish

Improve input and experience:

- Voice input.
- Richer generated backgrounds.
- Accessibility improvements.
- Performance tuning.
- Later: full video understanding.

## Success Criteria

- A new user understands how to start without needing a chat prompt.
- Predicted bubbles reduce typing and make intent selection feel faster than chat.
- Generated pages feel useful enough to bookmark or rate.
- Follow-up bubbles naturally drive the next interaction.
- Profile learning visibly improves suggestions after several sessions.

## Open Questions

- Should the first demo use a specific vertical, such as planning, research, shopping, or design?
- Should the UI use React, vanilla web components, or another framework?
- Should generated pages be editable by the user after creation?
- How much profile learning should happen automatically versus only through explicit user actions?
- Should generated pages be private by default with optional sharing later?
- Which reusable templates should be built first beyond list, card, comparison, chart, and diagram templates?
