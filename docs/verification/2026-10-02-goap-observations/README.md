# Observed GOAP effects — 2026-10-02

The compiled and dynamic planning paths execute two dependent personal file tasks.
Actual model inference sums expenses `[12,9,4]` to `25`, saves that JSON, then reads
it and writes `double_total=50`. Host arithmetic and actual file reads independently
check both outputs. Each run requires two distinct observation scopes, two verified
write/readback receipts, retained GoapChecks and a recomputable final result check.
A separate live case checks model result `total=42`; it claims no external effect.

Run `scripts/test-live-benchmarks.sh` for real-model qualification. `RealLLM` uses
the qualified local Ollama model with actual Sol 6.1 fallback on configured latency
or availability failure. Provider/call evidence in each retained report determines
which model established that run. Short protocol tests do not establish model
capability or promotion evidence.

Earlier failures are retained: old effect-copying test expectations; a native-file
fixture rejected by the existing output-quality backstop; mismatched model-output
versus committed-file digests; numeric oracle rounding during tree conversion;
initial lint findings; and a live result parser rejecting a Markdown-fenced correct
value. Fixes retain the exact arithmetic/effect requirements. Final suite, race and lint receipts are included with this checkpoint.

The initial successful dependent-file run used actual Sol fallback after an Ollama
timeout. The next successful dependent-file run is in `dependent-files-v2.json`;
its separately added value test exposed the formatting-only parser failure retained
in `live-value-parser-failure.txt`.

Limits: these are local fixtures, not production service rollout, wall-clock cron,
general personal-assistant competence or causal research impact. Built-in generic
research/DevOps declarations still need actual capability adapters. Legacy persisted
wrappers need reviewed regeneration/version adoption; complete GOAP configuration
preservation under mutation and durable external retry recovery remain open.

Verified results on the final code:

| Check | Observed result |
|---|---|
| Eight affected-package short suites | Passed; the later strict GOAP JSON parser change is covered by the final core race suite |
| Core race suites | goap, evolution and engine passed |
| Changed-package lint | Zero issues across goap, evolution, engine and benchmark |
| Real-model benchmark package | Passed in 152.761 seconds |
| Gardener live integration | Passed in 16.784 seconds |
| Factory/publication/personal-automation live integration | Passed in 65.997 seconds; measured publication, adoption and rollback retained |
| Dependent GOAP files | Compiled 31.05 seconds, dynamic 9.99 seconds; both used actual gpt-6.1-sol after one local-model timeout fallback |
| Exact model result | qwen2.5:1.5b, one call, no fallback, total=42 in 1.16 seconds |
| Documentation | Eleven checker regressions plus structure/links/traceability passed |
| Graphify | AST update: 33,256 nodes, 43,048 edges, 1,262 files; no model calls |

The project-conventions review found no new higher-layer engine imports, resurrected
packages, database persistence or unbounded shared JSON writes. Observation state is
run-local; retained terminal records use the existing atomic store. Runtime failure
after a committed FileTask write stops replay. No production services were restarted.
PR83 was fetched again; head `af780cdbf4745432b4b3a2d890a9c9e88443237e` is already an
ancestor of this branch. No newer PR83 commits existed at the check.
