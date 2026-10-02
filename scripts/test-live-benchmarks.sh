#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.."
state_root=$(mktemp -d /tmp/bt-live-benchmarks.XXXXXX)
trap 'rm -rf -- "$state_root"' EXIT
export BT_AGENT_HOME="$state_root/agent"
export BT_REFLECTIONS_DIR="$state_root/reflections"
export BT_MUTATED_TREES_DIR="$state_root/trees"
export PATH="/usr/local/go/bin:$PATH"
unset BT_SKIP_LLM_TESTS
export BT_BENCHMARK_REPORT="${BT_BENCHMARK_REPORT:-$PWD/test-results/live-governance.json}"
mkdir -p -- "$(dirname -- "$BT_BENCHMARK_REPORT")"

go test ./internal/benchmark -count=1 -timeout 300s -v \
  -run '^(TestLive|TestRunSuite_PathMatchRate|TestRunSuiteReportsUnsupported|TestABTest_|TestScoreMutation_|TestQuickValidate|TestLoadBFCLSuiteAndEvaluate|TestBFCLV3LoadFlattenAndEvaluate|TestGAIABuiltinAndEvaluation)'
go test ./cmd/bt-gardener -count=1 -timeout 120s -v \
  -run '^TestGardenerRunCycleTool_RejectsUnqualifiedSelectorOrdering'
go test ./cmd/bt-agent -count=1 -timeout 180s -v \
  -run '^(TestLiveFactory(CreatesResolvesAndExecutesTask|EvolutionPromotesAndRollsBackMeasuredVersion)|TestLiveManualAndGeneticPublication|TestLivePersonalFileAutomation)$'
