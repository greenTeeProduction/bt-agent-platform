# ADR-005: Stockfish-Adapted Evolution Engine

**Status:** Accepted
**Date:** 2026-05-27
**Deciders:** Nico (via Hermes Agent)

## Context

Behavior trees degrade over time without active maintenance — keyword overlap, dead paths, and structural bloat accumulate. Manual optimization doesn't scale to 46 trees across 7 categories. Options:

1. **Random mutations** — simple but 97.3% regression rate observed in production (144/148 mutations harmful)
2. **Grid search** — exhaustive but combinatorially explosive for trees with 50+ nodes
3. **Stockfish-adapted evolutionary search** — chess engine techniques repurposed for BT optimization

## Decision

Adapt Stockfish's search architecture to behavior tree evolution:

- **Transposition Table**: SHA256(tree+fitness) → cached evaluation, skip re-evaluation of seen trees
- **Killer Move Heuristic**: Mutations that improved fitness get priority in ordering
- **History Heuristic**: Mutations successful across multiple trees receive higher scores
- **Alpha-Beta Pruning**: Prune mutation branches that cannot beat current best composite score
- **Iterative Deepening**: Progressively increase mutation search depth (1→5 mutation combos)
- **Late Move Reductions**: Search promising mutations deeper (3 combo ops), prune unpromising (single pass)

Combine with 5 additional algorithm engines:
- **Genetic Algorithm**: Population-based (k=3 tournament, elitism, 30% mutation rate)
- **Q-Learning**: State→Action mapping with epsilon-greedy exploration (ε=0.2)
- **Decision Tree Optimizer**: C4.5/CART Information Gain and Gini impurity on Selector nodes
- **Ensemble Methods**: Voting, weighted, stacking across independently evolved trees
- **Memetic Local Search**: Post-GA Hill Climbing, Simulated Annealing, Tabu Search

Quality gates enforce: minimum composite floor (0.3), max regression tolerance (20%), auto-disable after 5 consecutive failures.

## Consequences

- **Positive**: Stockfish heuristics reduce search space by ~80% compared to random exploration
- **Positive**: Quality gates eliminated the 97.3% regression rate (commit `1c6ebd4d`)
- **Positive**: 24/7 gardener daemon runs cycles every 5 minutes across all registered trees
- **Positive**: Git versioning enables rollback of any harmful mutation
- **Negative**: Transposition table requires explicit `ev_tt_save()` — not auto-persisted (known gap)
- **Negative**: Full evaluation with real Ollama takes 3+ min/tree on Jetson; mock validation used for speed
