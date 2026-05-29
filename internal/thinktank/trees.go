package thinktank

import (
	"fmt"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// FellowResearchTree builds a behavior tree that researches a topic from ONE specific
// fellow's perspective. The fellow's Persona is injected as the system_msg so each
// ChainAction node embodies the fellow's analytical style.
func FellowResearchTree(fellow Fellow, topic string) *evolution.SerializableNode {
	sysMsg := fellow.Persona
	if sysMsg == "" {
		sysMsg = fmt.Sprintf("You are %s, a %s analyst. Expertise: %s. Perspective: %s.",
			fellow.Name, fellow.Role, fellow.Expertise, fellow.Perspective)
	}

	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: fmt.Sprintf("FellowResearch_%s", fellow.Name),
		Children: []evolution.SerializableNode{
			// PreGate: set up research tools
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{
						Type: "Condition",
						Name: "ValidateCompanyState",
						Description: "Ensure thinktank is on the blackboard",
					},
					{
						Type: "Action",
						Name: "SetupResearchTools",
						Description: "Populate bb.ChainTools with web_search, knowledge_graph, calculator",
					},
				},
			},
			// DeepResearch: thoroughly investigate the topic through the fellow's lens
			{
				Type: "ChainAction",
				Name: fmt.Sprintf("llm_call:You are %s (%s analyst). Research the topic '%s' from your unique perspective: %s. Use your expertise in %s to identify key insights, supporting data, and critical factors. Produce a structured analysis with clearly labeled sections: KEY INSIGHTS, EVIDENCE, ASSUMPTIONS, and RECOMMENDATION.",
					fellow.Name, fellow.Role, topic, fellow.Perspective, fellow.Expertise),
				Metadata: map[string]any{
					"system_msg": sysMsg,
					"max_tokens": float64(4096),
					"tools": []any{"web_search", "knowledge_graph", "calculator"},
				},
			},
			// BiasCheck: identify own biases and blind spots
			{
				Type: "ChainAction",
				Name: fmt.Sprintf("llm_call:You are %s. Review your own analysis on '%s'. Identify your cognitive biases, unchallenged assumptions, and analytical blind spots. Be brutally honest about where your perspective might be distorting your conclusions. List specific biases and how they may have influenced your findings.", fellow.Name, topic),
				Metadata: map[string]any{
					"system_msg": sysMsg,
					"max_tokens": float64(2048),
				},
			},
			// EvidenceCollection: gather supporting AND contradicting evidence
			{
				Type: "ChainAction",
				Name: fmt.Sprintf("llm_call:You are %s. For your analysis of '%s', systematically collect evidence that BOTH supports AND contradicts your thesis. For each piece of evidence, rate its strength (strong/moderate/weak) and source reliability. If you find strong contradicting evidence, acknowledge it and explain how it affects your confidence. Format as: SUPPORTING EVIDENCE: ... CONTRADICTING EVIDENCE: ...", fellow.Name, topic),
				Metadata: map[string]any{
					"system_msg": sysMsg,
					"max_tokens": float64(3072),
					"tools": []any{"web_search"},
				},
			},
			// ProduceFinding: output a structured ResearchFinding
			{
				Type: "ChainAction",
				Name: fmt.Sprintf("llm_call:You are %s. Synthesize your research on '%s' into a final structured finding. Output your answer as a JSON object with these fields: fellow_name (string), key_insights (array of strings), evidence (array of strings), assumptions (array of strings), confidence (number 0-1), recommendation (string), risks (array of strings). Be specific, cite your evidence, and be honest about your confidence level.", fellow.Name, topic),
				Metadata: map[string]any{
					"system_msg": sysMsg,
					"max_tokens": float64(3072),
				},
			},
		},
	}
}

// DebateTree orchestrates a dialectic debate between all fellows on the given topic.
// Follows the structured debate format: opening statements, cross-examination, rebuttal,
// synthesis moves, and a final vote.
func DebateTree(fellows []Fellow, topic string) *evolution.SerializableNode {
	// Build the debate children dynamically based on the number of fellows
	children := make([]evolution.SerializableNode, 0, 12)

	// PreGate
	children = append(children, evolution.SerializableNode{
		Type: "Sequence",
		Name: "PreGate",
		Children: []evolution.SerializableNode{
			{
				Type: "Condition",
				Name: "ValidateCompanyState",
				Description: "Ensure thinktank is on the blackboard",
			},
			{
				Type: "Action",
				Name: "SetupResearchTools",
				Description: "Populate bb.ChainTools for debate research",
			},
		},
	})

	// Round 1: Opening Statements — each fellow states their thesis
	openingChildren := make([]evolution.SerializableNode, len(fellows))
	for i, f := range fellows {
		sys := f.Persona
		if sys == "" {
			sys = fmt.Sprintf("You are %s, a %s analyst.", f.Name, f.Role)
		}
		openingChildren[i] = evolution.SerializableNode{
			Type: "ChainAction",
			Name: fmt.Sprintf("llm_call:You are %s (%s analyst, confidence: %.0f%%). State your opening thesis on '%s'. Articulate your core argument clearly and concisely. Explain the key evidence that supports your position. Be persuasive but intellectually honest. This is a debate — take a clear stance.", f.Name, f.Role, f.Confidence*100, topic),
			Metadata: map[string]any{
				"system_msg": sys,
				"max_tokens": float64(2048),
			},
		}
	}
	children = append(children, evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "OpeningStatements",
		Children: openingChildren,
	})

	// Round 2: Cross-Examination — fellows challenge each other
	crossChildren := make([]evolution.SerializableNode, len(fellows))
	for i, f := range fellows {
		sys := f.Persona
		if sys == "" {
			sys = fmt.Sprintf("You are %s, a %s analyst.", f.Name, f.Role)
		}
		// Pick a different fellow to challenge
		challengeIdx := (i + 1) % len(fellows)
		crossChildren[i] = evolution.SerializableNode{
			Type: "ChainAction",
			Name: fmt.Sprintf("llm_call:You are %s. You have just heard arguments from your fellow analysts, especially %s. Challenge their position on '%s'. Identify weaknesses in their reasoning, gaps in their evidence, or assumptions they haven't examined. Ask probing questions. Be rigorous but respectful.", f.Name, fellows[challengeIdx].Name, topic),
			Metadata: map[string]any{
				"system_msg": sys,
				"max_tokens": float64(2048),
			},
		}
	}
	children = append(children, evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "CrossExamination",
		Children: crossChildren,
	})

	// Round 3: Rebuttal — each fellow defends and refines their position
	rebuttalChildren := make([]evolution.SerializableNode, len(fellows))
	for i, f := range fellows {
		sys := f.Persona
		if sys == "" {
			sys = fmt.Sprintf("You are %s, a %s analyst.", f.Name, f.Role)
		}
		rebuttalChildren[i] = evolution.SerializableNode{
			Type: "ChainAction",
			Name: fmt.Sprintf("llm_call:You are %s. You have been challenged on your position regarding '%s'. Defend your thesis. Address the criticisms directly: which points do you concede, and which do you refute with stronger evidence? Refine your argument based on what you've learned. Acknowledge valid counterpoints while strengthening your core thesis.", f.Name, topic),
			Metadata: map[string]any{
				"system_msg": sys,
				"max_tokens": float64(2048),
			},
		}
	}
	children = append(children, evolution.SerializableNode{
		Type:     "Sequence",
		Name:     "Rebuttal",
		Children: rebuttalChildren,
	})

	// Synthesis Move: find common ground across all perspectives
	children = append(children, evolution.SerializableNode{
		Type: "ChainAction",
		Name: fmt.Sprintf("llm_call:You are the debate moderator. After hearing opening statements, cross-examination, and rebuttals from %d fellows on '%s', identify areas of common ground. What do all (or most) perspectives agree on? Where are the irreducible disagreements? Map the landscape of consensus and conflict. List: POINTS OF AGREEMENT, POINTS OF DISAGREEMENT, UNRESOLVED TENSIONS.", len(fellows), topic),
		Metadata: map[string]any{
			"system_msg": "You are an expert debate moderator and dialectical synthesizer. You are skilled at finding common ground between opposing viewpoints while being honest about genuine disagreements. You do not take sides but map the argument landscape clearly.",
			"max_tokens": float64(3072),
		},
	})

	// CallVote: assess which arguments have strongest evidence
	children = append(children, evolution.SerializableNode{
		Type: "ChainAction",
		Name: fmt.Sprintf("llm_call:You are the debate judge. Evaluate the debate on '%s' involving %d fellows. For each major argument presented, assess: (1) Quality of evidence, (2) Logical coherence, (3) Predictive power. Rank the arguments by strength. Identify which positions are most defensible and which are weakest. Output as: STRONGEST ARGUMENTS: ... WEAKEST ARGUMENTS: ... OVERALL ASSESSMENT: ...", topic, len(fellows)),
		Metadata: map[string]any{
			"system_msg": "You are an impartial debate judge with expertise in epistemology and argumentation. You evaluate arguments based on evidence quality, logical rigor, and explanatory power. You are fair but decisive.",
			"max_tokens": float64(2048),
		},
	})

	return &evolution.SerializableNode{
		Type:     "Sequence",
		Name:     fmt.Sprintf("Debate_%s", topic),
		Children: children,
	}
}

// SynthesisTree combines all research findings and debate transcripts into a unified
// Synthesis using dialectical reasoning (thesis → antithesis → synthesis).
func SynthesisTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Synthesis",
		Children: []evolution.SerializableNode{
			// IdentifyThesis: dominant view from findings
			{
				Type: "ChainAction",
				Name: "llm_call:Review all the research findings from the think tank fellows. Identify the DOMINANT THESIS — the most widely supported or most strongly argued position. What is the central claim that most evidence points toward? State it clearly in 1-2 sentences, then list the supporting evidence and which fellows support it. Output as: DOMINANT THESIS: ... SUPPORTING EVIDENCE: ... SUPPORTING FELLOWS: ...",
				Metadata: map[string]any{
					"system_msg": "You are a senior research synthesizer. You excel at identifying the central thesis across multiple analytical perspectives. You are precise, evidence-focused, and avoid false balance.",
					"max_tokens": float64(2048),
				},
			},
			// IdentifyAntithesis: strongest counterargument
			{
				Type: "ChainAction",
				Name: "llm_call:Review all research findings and debate transcripts. Identify the strongest ANTITHESIS — the most compelling counterargument to the dominant view. What is the strongest case against the dominant thesis? State it clearly, cite the opposing evidence, and identify which fellows advance this position. Output as: ANTITHESIS: ... OPPOSING EVIDENCE: ... OPPOSING FELLOWS: ...",
				Metadata: map[string]any{
					"system_msg": "You are a senior research synthesizer specializing in identifying counterarguments. You give minority views their strongest possible articulation and ensure they are not dismissed prematurely.",
					"max_tokens": float64(2048),
				},
			},
			// ResolveDialectic: produce synthesis position
			{
				Type: "ChainAction",
				Name: "llm_call:You have identified the thesis and antithesis. Now produce a SYNTHESIS — a higher-level position that integrates the valid insights from both sides while transcending their limitations. This is a Hegelian dialectical move: the synthesis should not just be a compromise but a genuinely new, more comprehensive understanding. State the synthesis clearly. Explain what it preserves from each side and what it rejects. Output as: SYNTHESIS: ... INTEGRATION: ... PRESERVED FROM THESIS: ... PRESERVED FROM ANTITHESIS: ... RESOLVED TENSIONS: ...",
				Metadata: map[string]any{
					"system_msg": "You are a master of dialectical reasoning. You produce syntheses that are not compromises but genuine advances — positions that integrate opposing valid insights while resolving their contradictions at a higher level of understanding.",
					"max_tokens": float64(3072),
				},
			},
			// MapAgreement/Disagreement: catalog alignment gaps
			{
				Type: "ChainAction",
				Name: "llm_call:Based on all research findings and debate transcripts, catalog the POINTS OF AGREEMENT (where 3+ fellows converge) and POINTS OF DISAGREEMENT (where perspectives irreducibly diverge). For each disagreement, note whether it's a factual dispute (resolvable with more data) or a values/interpretation dispute (genuinely irreducible). Output as two clearly labeled sections.",
				Metadata: map[string]any{
					"system_msg": "You are a meticulous research cartographer. You map the landscape of consensus and disagreement with precision, distinguishing between resolvable factual disputes and irreducible value/interpretation differences.",
					"max_tokens": float64(2048),
				},
			},
			// ProduceRecommendation: actionable recommendation
			{
				Type: "ChainAction",
				Name: "llm_call:Based on the synthesis, produce a clear, actionable RECOMMENDATION. This should be a specific course of action (or set of options) that follows from the analysis. Include: (1) Primary recommendation with rationale, (2) Alternative options with trade-offs, (3) Confidence level (low/medium/high) with justification, (4) Key conditions or triggers that would change the recommendation. Output as: RECOMMENDATION: ... RATIONALE: ... ALTERNATIVES: ... CONFIDENCE: ... CONDITIONAL TRIGGERS: ...",
				Metadata: map[string]any{
					"system_msg": "You are a strategic advisor who translates complex analysis into clear, actionable recommendations. You are specific, practical, and honest about uncertainty. You provide decision-makers with what they need to act.",
					"max_tokens": float64(3072),
				},
			},
			// RecordDissentingNotes: capture minority views
			{
				Type: "ChainAction",
				Name: "llm_call:Identify and record DISSENTING NOTES — important minority viewpoints that are not reflected in the primary recommendation but deserve to be preserved. For each dissenting note, state: (1) The view, (2) Which fellow(s) hold it, (3) Why it was not incorporated into the main recommendation, (4) Under what conditions this minority view might prove correct. Be respectful — these are serious analysts whose views should not be lost.",
				Metadata: map[string]any{
					"system_msg": "You are a careful steward of intellectual diversity. You ensure that minority viewpoints are preserved and given their due weight, even when the preponderance of evidence supports a different conclusion.",
					"max_tokens": float64(2048),
				},
			},
		},
	}
}

// PeerReviewTree builds a behavior tree that has each fellow review the synthesis
// from their perspective, checking for factual errors, logical fallacies, bias,
// and evidence gaps.
func PeerReviewTree() *evolution.SerializableNode {
	// The peer review runs for each fellow perspective
	// We use a sequence of review chains — one per analytical role
	reviewRoles := []struct {
		Name    string
		Role    string
		System  string
		Focus   string
	}{
		{
			Name:   "Fact Check (Bull lens)",
			Role:   "bull",
			System: "You are a rigorous fact-checker with an optimistic but evidence-driven perspective. You verify claims, check sources, and ensure factual accuracy while being open to upside potential.",
			Focus:  "Verify the key factual claims in the synthesis. Are all cited facts accurate? Are sources credible? Are there any misrepresentations or exaggerations? Check each claim against what you know and flag anything questionable.",
		},
		{
			Name:   "Fact Check (Bear lens)",
			Role:   "bear",
			System: "You are a skeptical fact-checker who stress-tests rosy assumptions. You verify claims with extra scrutiny on optimistic projections and ensure downside risks are not understated.",
			Focus:  "Verify the key factual claims from a skeptical perspective. Are downside risks adequately represented? Are optimistic projections well-grounded in evidence? Flag any claims that seem insufficiently supported.",
		},
		{
			Name:   "Logic Check",
			Role:   "logician",
			System: "You are a logician specializing in identifying fallacies, weak inferences, and flawed reasoning. You examine arguments for structural validity, not factual accuracy.",
			Focus:  "Examine the logical structure of the synthesis. Identify any logical fallacies (straw man, false dichotomy, post hoc, etc.), invalid inferences, or reasoning gaps. For each issue found, explain why it's problematic.",
		},
		{
			Name:   "Bias Audit",
			Role:   "bias_auditor",
			System: "You are a cognitive bias expert. You detect unexamined assumptions, groupthink, anchoring effects, confirmation bias, and other cognitive distortions in analytical work.",
			Focus:  "Audit the synthesis for cognitive biases and unexamined assumptions. Does the synthesis show signs of groupthink? Anchoring on initial findings? Confirmation bias toward the dominant view? Overconfidence? Flag every potential bias you detect.",
		},
		{
			Name:   "Evidence Gap Analysis",
			Role:   "evidence_analyst",
			System: "You are an evidence-quality expert. You assess whether conclusions are adequately supported by the evidence presented and identify crucial missing data.",
			Focus:  "Identify EVIDENCE GAPS in the synthesis. What important data is missing? What would we need to know to increase confidence? What crucial experiments, data sources, or analyses were not conducted? Rate each gap by severity (critical/major/minor).",
		},
	}

	reviewChildren := make([]evolution.SerializableNode, len(reviewRoles))
	for i, r := range reviewRoles {
		reviewChildren[i] = evolution.SerializableNode{
			Type: "ChainAction",
			Name: fmt.Sprintf("llm_call:%s: %s", r.Name, r.Focus),
			Metadata: map[string]any{
				"system_msg": r.System,
				"max_tokens": float64(2048),
			},
		}
	}

	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "PeerReview",
		Children: []evolution.SerializableNode{
			// PreGate
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{
						Type:        "Condition",
						Name:        "ValidateCompanyState",
						Description: "Ensure thinktank and synthesis exist",
					},
				},
			},
			// Run all review perspectives sequentially
			{
				Type:     "Sequence",
				Name:     "ReviewRounds",
				Children: reviewChildren,
			},
			// Produce consolidated review comments
			{
				Type: "ChainAction",
				Name: "llm_call:You are the peer review coordinator. Consolidate all the review findings into a structured set of review comments. For each issue found (factual errors, logical fallacies, biases, evidence gaps), create a review entry with: reviewer name, section affected, issue type, severity (critical/major/minor), the specific issue found, and a suggested fix. Output as a numbered list of review comments.",
				Metadata: map[string]any{
					"system_msg": "You are a meticulous peer review coordinator. You consolidate feedback from multiple reviewers into clear, actionable review comments. You are organized, precise, and constructive.",
					"max_tokens": float64(3072),
				},
			},
		},
	}
}

// ReportGenerationTree builds a behavior tree that produces the final Report
// from the synthesis and peer review feedback.
func ReportGenerationTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "ReportGeneration",
		Children: []evolution.SerializableNode{
			// ExecutiveSummary
			{
				Type: "ChainAction",
				Name: "llm_call:Write the EXECUTIVE SUMMARY for the think tank report. This should be a self-contained 2-3 paragraph summary that a busy executive can read in 60 seconds and understand: (1) The key question, (2) The main finding, (3) The recommendation, (4) The confidence level, (5) The most critical risk. Be concise, clear, and impactful. Start with the bottom line.",
				Metadata: map[string]any{
					"system_msg": "You are an executive communications expert. You write clear, concise summaries that busy decision-makers can act on. You lead with conclusions, not process. Every sentence earns its place.",
					"max_tokens": float64(2048),
				},
			},
			// Background
			{
				Type: "ChainAction",
				Name: "llm_call:Write the BACKGROUND section of the report. Explain the context: why this topic matters, what prompted this analysis, the scope of investigation, the analytical approach used (multi-perspective think tank with dialectical debate), and any key definitions or frameworks the reader needs to understand the analysis. Be thorough but not tedious.",
				Metadata: map[string]any{
					"system_msg": "You are a research writer who excels at contextualizing complex analysis. You provide the background a reader needs without overwhelming them with unnecessary detail.",
					"max_tokens": float64(2048),
				},
			},
			// ScenarioAnalysis: generate 3-5 scenarios
			{
				Type: "ChainAction",
				Name: "llm_call:Based on the synthesis and research findings, generate 3-5 SCENARIOS for how the situation could unfold. For each scenario, provide: (1) Name — a memorable label, (2) Description — what happens and why, (3) Probability estimate (0-100%), (4) Impact (high/medium/low), (5) Key triggers or signposts that would indicate this scenario is unfolding, (6) Recommended response if this scenario materializes. Include at least one optimistic, one pessimistic, and one unexpected/wildcard scenario. Output each scenario clearly.",
				Metadata: map[string]any{
					"system_msg": "You are a scenario planning expert. You generate plausible, well-reasoned alternative futures that challenge assumptions and prepare decision-makers for a range of outcomes. You are creative but grounded in evidence.",
					"max_tokens": float64(4096),
				},
			},
			// Recommendation with confidence level
			{
				Type: "ChainAction",
				Name: "llm_call:Write the RECOMMENDATION section of the report. Present the primary recommendation with clear rationale. State the confidence level (high/medium/low) and explain what drives that confidence. Discuss alternative courses of action and their trade-offs. Be specific about what action to take, by whom, and when. This should be the most actionable section of the report.",
				Metadata: map[string]any{
					"system_msg": "You are a strategic decision advisor. Your recommendations are specific, actionable, and honest about uncertainty. You tell decision-makers not just what to do, but why, and what could go wrong.",
					"max_tokens": float64(2048),
				},
			},
			// RisksAndCaveats
			{
				Type: "ChainAction",
				Name: "llm_call:Write the RISKS AND CAVEATS section. Identify the key risks to the recommendation: what could go wrong, how likely each risk is, and what the impact would be. Include both internal risks (flawed assumptions, missing data) and external risks (market shifts, regulatory changes, geopolitical events). Be honest about the limits of this analysis — what we don't know, what we couldn't verify, and what could change the conclusion. This is not a CYA section; it's genuine intellectual honesty.",
				Metadata: map[string]any{
					"system_msg": "You are a risk analyst who takes intellectual honesty seriously. You identify risks thoroughly but proportionally — you don't inflate minor risks to seem thorough, and you don't downplay major risks to seem confident.",
					"max_tokens": float64(2048),
				},
			},
			// NextSteps
			{
				Type: "ChainAction",
				Name: "llm_call:Write the NEXT STEPS section. What should happen after this report? Identify: (1) Immediate actions (next 1-4 weeks), (2) Short-term monitoring (1-3 months), (3) Longer-term research agenda (3-12 months), (4) Trigger points for revisiting the analysis, (5) Who should be briefed or involved next. Make each next step specific and assignable.",
				Metadata: map[string]any{
					"system_msg": "You are a project execution specialist. You translate analysis into concrete next actions with clear owners, timelines, and success criteria.",
					"max_tokens": float64(2048),
				},
			},
		},
	}
}
