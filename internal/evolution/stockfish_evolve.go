package evolution

// StockfishEvolutionTree is a meta-evolution tree that uses Stockfish-adapted chess
// algorithms to improve behavior trees. It treats tree mutation as a search problem:
//
//   - Transposition Table: cache fitness evaluations to avoid re-computation
//   - Killer Moves: mutations that worked recently get priority
//   - History Heuristic: track which mutations help which tree types
//   - Iterative Deepening: search shallow first, then deeper
//   - Late Move Reductions: prune unpromising mutation branches
//   - Alpha-Beta Pruning: skip mutations that can't beat current best
//
// This tree runs the FULL Stockfish pipeline:
//   Evaluate → OrderMutations(Killer+History) → Deepen → Prune → Apply → Validate → TT Store → Repeat
func StockfishEvolutionTree() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "StockfishEvolve_Main",
		Children: []SerializableNode{
			// Phase 0: Setup
			{
				Type: "Sequence",
				Name: "SetupPhase",
				Children: []SerializableNode{
					{Type: "Condition", Name: "ValidateInput"},
					{Type: "Action", Name: "SetupDefaultTools"},
					{Type: "Action", Name: "InitTranspositionTable"},
				},
			},

			// Phase 1: Load state and check TT cache
			{
				Type: "Sequence",
				Name: "TranspositionLookup",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Hash the current behavior tree and look up its fitness score in the transposition table. If found and recent, skip re-evaluation and use cached score. Report: cache_hit or cache_miss.",
						Metadata: map[string]any{
							"max_tokens": float64(3),
							"system_msg": "You are a Stockfish-style transposition table manager.",
						},
					},
				},
			},

			// Phase 2: Evaluate fitness (if TT miss)
			{
				Type: "Selector",
				Name: "FitnessGate",
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "UseCachedFitness",
						Children: []SerializableNode{
							{Type: "Condition", Name: "HasCachedFitness"},
							{Type: "Action", Name: "LoadCachedFitness"},
						},
					},
					{
						Type: "Sequence",
						Name: "ComputeFreshFitness",
						Children: []SerializableNode{
							{
								Type: "ChainAction",
								Name: "llm_call:Evaluate the behavior tree's fitness across 5 dimensions: Success Rate (50%), Stability (15%), Path Coverage (15%), Speed (10%), Complexity (10%). Compute composite score on 0-100 centipawn scale. Report: composite score and per-dimension breakdown.",
								Metadata: map[string]any{
									"max_tokens": float64(5),
									"system_msg": "You are a Stockfish evaluator. Score behavior trees like chess positions. Higher is better.",
								},
							},
							{Type: "Action", Name: "StoreInTranspositionTable"},
						},
					},
				},
			},

			// Phase 3: Iterative Deepening loop
			{
				Type: "Sequence",
				Name: "IterativeDeepening",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Run iterative deepening mutation search. Start at depth 1 (single mutations), increase to depth 3 (mutation chains). At each depth: generate candidate mutations, order them by Stockfish heuristics, evaluate top candidates, prune weak branches. Best mutation found so far is the 'principal variation'. Report: best mutation, its score, and depth reached.",
						Metadata: map[string]any{
							"max_tokens": float64(12),
							"system_msg": "You are an iterative deepening search engine for behavior tree optimization. Search wider at shallow depths, deeper on promising branches.",
						},
					},
				},
			},

			// Phase 4: Move Ordering (Killer Moves + History Heuristic)
			{
				Type: "Sequence",
				Name: "MoveOrdering",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Order the candidate mutations using Stockfish move ordering: 1. TT hits (previously evaluated positively), 2. Killer moves (recent successful mutations), 3. History heuristic (mutations with high success rate on similar trees), 4. Capture-like (add_fallback, wrap_retry — structural improvements), 5. Quiet moves (increase_retries, add_before — parameter tweaks). Report ranked list with scores.",
						Metadata: map[string]any{
							"max_tokens": float64(8),
							"system_msg": "You are a Stockfish move ordering engine. Rank mutations by likelihood of improving fitness.",
						},
					},
				},
			},

			// Phase 5: Alpha-Beta Pruning
			{
				Type: "Sequence",
				Name: "AlphaBetaPruning",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Apply alpha-beta pruning to the candidate mutations. Establish alpha (best score so far) and beta (opponent's best refutation). Prune any mutation whose upper bound is below alpha. Skip mutations that are provably worse than the current best tree. Report: pruned count and surviving candidates.",
						Metadata: map[string]any{
							"max_tokens": float64(5),
							"system_msg": "You are an alpha-beta pruning engine. Eliminate mutations that can't improve the tree.",
						},
					},
				},
			},

			// Phase 6: Late Move Reductions
			{
				Type: "Sequence",
				Name: "LateMoveReductions",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Apply late move reductions to the remaining candidates. Mutations late in the ordered list get reduced search depth (fewer benchmark tasks, faster evaluation). Only mutations in the top half of the ordered list get full-depth evaluation. This speeds up search without missing strong moves.",
						Metadata: map[string]any{
							"max_tokens": float64(5),
							"system_msg": "You are a late move reduction engine. Save time on unlikely candidates.",
						},
					},
				},
			},

			// Phase 7: Apply and benchmark-validate the best mutation
			{
				Type: "Sequence",
				Name: "ApplyAndValidate",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Apply the best mutation to the behavior tree. Then validate it against benchmark suites (BFCL routing, SWE-bench resolution, τ-bench task completion). Compute the fitness delta. If positive, keep the mutation. If negative, revert and try the next candidate. Report: mutation applied, fitness delta, and whether it improved.",
						Metadata: map[string]any{
							"max_tokens": float64(8),
							"system_msg": "You are a mutation validator. Only keep mutations that provably improve benchmark scores.",
						},
					},
				},
			},

			// Phase 8: Update Stockfish heuristics
			{
				Type: "Sequence",
				Name: "UpdateHeuristics",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Update the Stockfish heuristics based on results: if mutation succeeded, increment history score for this mutation type on this tree type. If it was a new best, mark it as a killer move. Store the new fitness in the transposition table. Age old entries to make room for recent data.",
						Metadata: map[string]any{
							"max_tokens": float64(5),
							"system_msg": "You are a heuristic update engine. Learn from each mutation attempt.",
						},
					},
				},
			},

			// Phase 9: Generate evolution report
			{
				Type: "Sequence",
				Name: "ReportPhase",
				Children: []SerializableNode{
					{
						Type: "ChainAction",
						Name: "llm_call:Generate a Stockfish Evolution Report summarizing: trees evaluated, mutations searched (with pruning stats), best mutation found, fitness improvement, transposition table hit rate, killer moves identified, and recommendations for the next evolution cycle.",
						Metadata: map[string]any{
							"max_tokens": float64(8),
							"system_msg": "You are a chess engine reporting on a search. Same format: depth reached, nodes searched, principal variation, evaluation.",
						},
					},
					{Type: "Action", Name: "ReflectOnOutcome"},
				},
			},

			// Outcome handling
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "ChainAction",
						Name: "llm_call:Evolution attempt failed. Analyze why: was the search depth insufficient? Were all mutations pruned? Was the benchmark suite too narrow? Recommend parameter adjustments for the next cycle.",
						Metadata: map[string]any{"max_tokens": float64(5)},
					},
				},
			},
		},
	}
}

// StockfishEvolutionLoop runs continuous tree evolution using Stockfish algorithms.
// It's an infinite loop that:
//   1. Evaluates all registered trees
//   2. Runs StockfishEvolutionTree on the lowest-fitness tree
//   3. Updates heuristics and transposition table
//   4. Sleeps for the configured interval
//   5. Repeats forever
//
// This is the infinite improvement loop — it never stops, only gets better.
func StockfishEvolutionLoop() *SerializableNode {
	return &SerializableNode{
		Type: "Sequence",
		Name: "StockfishLoop_Main",
		Children: []SerializableNode{
			// Initialize
			{
				Type: "Sequence",
				Name: "LoopSetup",
				Children: []SerializableNode{
					{Type: "Action", Name: "SetupDefaultTools"},
					{Type: "Action", Name: "InitTranspositionTable"},
					{
						Type: "ChainAction",
						Name: "llm_call:Initialize the Stockfish evolution loop. Load all registered behavior trees, initialize the transposition table, killer move table, and history heuristic table. Set the evolution interval and max mutations per cycle.",
						Metadata: map[string]any{"max_tokens": float64(5)},
					},
				},
			},

			// Infinite loop body — uses Retry decorator with high max retries
			{
				Type: "Retry",
				Name: "InfiniteEvolveLoop",
				MaxRetries: 999999, // effectively infinite
				Children: []SerializableNode{
					{
						Type: "Sequence",
						Name: "LoopBody",
						Children: []SerializableNode{
							// Step 1: Scan all trees, find weakest
							{
								Type: "ChainAction",
								Name: "llm_call:Scan all behavior trees in the registry. Evaluate each tree's fitness score. Identify the weakest tree (lowest composite fitness). Also check transposition table for trees that haven't been evaluated recently. Report: weakest tree name, its fitness, and why it was selected.",
								Metadata: map[string]any{
									"max_tokens": float64(6),
									"system_msg": "You are a tree selection engine. Focus on the weakest link first.",
								},
							},
							// Step 2: Run Stockfish evolution on the weakest tree
							{
								Type: "ChainAction",
								Name: "llm_call:Run the Stockfish evolution pipeline on the selected tree: TT lookup → evaluate fitness → iterative deepening (depth 1-3) → move ordering (killer + history) → alpha-beta prune → late move reduce → apply best → benchmark validate → update heuristics. Report the full search summary.",
								Metadata: map[string]any{
									"max_tokens": float64(15),
									"system_msg": "You are a Stockfish search engine. Find the best mutation for this tree.",
								},
							},
							// Step 3: Apply if improved
							{
								Type: "Selector",
								Name: "ImprovementGate",
								Children: []SerializableNode{
									{
										Type: "Sequence",
										Name: "ApplyImprovement",
										Children: []SerializableNode{
											{Type: "Condition", Name: "HasFitnessImproved"},
											{
												Type: "ChainAction",
												Name: "llm_call:Save the improved tree to disk. Update the killer move table (this mutation type gets a bonus). Update the history heuristic table. Log the improvement with the fitness delta.",
												Metadata: map[string]any{"max_tokens": float64(4)},
											},
										},
									},
									{
										Type: "Sequence",
										Name: "SkipNoImprovement",
										Children: []SerializableNode{
											{
												Type: "ChainAction",
												Name: "llm_call:No improvement found. Decrease history score for these mutation types on this tree. If this tree has had no improvements for 10+ cycles, consider increasing search depth or trying a different mutation strategy.",
												Metadata: map[string]any{"max_tokens": float64(4)},
											},
										},
									},
								},
							},
							// Step 4: Periodic TT cleanup
							{
								Type: "ChainAction",
								Name: "llm_call:Every 10 cycles, clean the transposition table: remove entries older than 100 cycles, keep only the best score per tree, defragment. Report TT statistics: size, hit rate, age distribution.",
								Metadata: map[string]any{
									"max_tokens": float64(3),
									"system_msg": "You are a transposition table maintenance engine.",
								},
							},
							// Step 5: Cycle report
							{
								Type: "ChainAction",
								Name: "llm_call:Generate a cycle summary: trees processed, mutations tried, improvements found, total fitness gain across all trees, TT hit rate, killer moves active, and estimated time to convergence.",
								Metadata: map[string]any{
									"max_tokens": float64(6),
									"system_msg": "You are a Stockfish engine reporting search progress. Use chess terminology: depth, nodes, principal variation, evaluation in centipawns.",
								},
							},
						},
					},
				},
			},

			// Never reached in practice, but provides graceful shutdown
			{
				Type: "Action",
				Name: "ReflectOnOutcome",
			},
		},
	}
}
