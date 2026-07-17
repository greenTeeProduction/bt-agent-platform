package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/reliability"
)

// MessageKind discriminates the three auction message types exchanged during
// task allocation: announcement → bid → award.
type MessageKind string

const (
	// KindAnnouncement labels a TaskAnnouncement broadcast by an auctioneer.
	KindAnnouncement MessageKind = "task_announcement"
	// KindBid labels a Bid submitted by a candidate agent.
	KindBid MessageKind = "bid"
	// KindAward labels an Award naming the winning bidder.
	KindAward MessageKind = "award"
)

// ErrNoEligibleBids is returned by a BidEvaluator when no submitted bid is
// eligible to win the announced task (wrong task, or below min confidence).
var ErrNoEligibleBids = errors.New("a2a: no eligible bids for announced task")

// defaultBidDeadline bounds each candidate's fan-out call when the announcement
// carries no explicit Deadline, so an unbounded announcement can never let a
// single hung candidate stall the auction forever.
const defaultBidDeadline = 30 * time.Second

// TaskAnnouncement is broadcast to candidate agents to open an auction for a
// single unit of work.
type TaskAnnouncement struct {
	TaskID        string    `json:"task_id"`
	Description   string    `json:"description,omitempty"`
	RequiredTags  []string  `json:"required_tags,omitempty"`
	MinConfidence float64   `json:"min_confidence,omitempty"`
	Deadline      time.Time `json:"deadline,omitempty"`
}

// Kind identifies this message as a task announcement.
func (TaskAnnouncement) Kind() MessageKind { return KindAnnouncement }

// Validate reports whether the announcement is well-formed.
func (a TaskAnnouncement) Validate() error {
	if a.TaskID == "" {
		return fmt.Errorf("a2a: announcement TaskID is required")
	}
	if a.MinConfidence < 0 || a.MinConfidence > 1 {
		return fmt.Errorf("a2a: announcement MinConfidence %v out of range [0,1]", a.MinConfidence)
	}
	return nil
}

// Bid is a candidate agent's offer to perform an announced task.
type Bid struct {
	TaskID     string  `json:"task_id"`
	BidderName string  `json:"bidder_name"`
	Cost       float64 `json:"cost"`
	Confidence float64 `json:"confidence"`
}

// Kind identifies this message as a bid.
func (Bid) Kind() MessageKind { return KindBid }

// Validate reports whether the bid is well-formed.
func (b Bid) Validate() error {
	if b.TaskID == "" {
		return fmt.Errorf("a2a: bid TaskID is required")
	}
	if b.BidderName == "" {
		return fmt.Errorf("a2a: bid BidderName is required")
	}
	if b.Cost < 0 {
		return fmt.Errorf("a2a: bid Cost %v must be non-negative", b.Cost)
	}
	if b.Confidence < 0 || b.Confidence > 1 {
		return fmt.Errorf("a2a: bid Confidence %v out of range [0,1]", b.Confidence)
	}
	return nil
}

// Award names the winning bidder for an announced task.
type Award struct {
	TaskID     string `json:"task_id"`
	WinnerName string `json:"winner_name"`
	WinningBid Bid    `json:"winning_bid"`
}

// Kind identifies this message as an award.
func (Award) Kind() MessageKind { return KindAward }

// BidEvaluator selects the winning bid for an announced task.
type BidEvaluator interface {
	Evaluate(ann TaskAnnouncement, bids []Bid) (Award, error)
}

// ScoreEvaluator picks the eligible bid with the lowest cost, breaking ties in
// favor of higher confidence. A bid is eligible when it targets the announced
// task and meets the announcement's minimum confidence.
type ScoreEvaluator struct{}

// Evaluate implements BidEvaluator.
func (ScoreEvaluator) Evaluate(ann TaskAnnouncement, bids []Bid) (Award, error) {
	var winner *Bid
	for i := range bids {
		b := bids[i]
		if b.TaskID != ann.TaskID {
			continue // foreign-task bid
		}
		if b.Confidence < ann.MinConfidence {
			continue // below required confidence
		}
		if winner == nil || betterBid(b, *winner) {
			w := b
			winner = &w
		}
	}
	if winner == nil {
		return Award{}, ErrNoEligibleBids
	}
	return Award{
		TaskID:     ann.TaskID,
		WinnerName: winner.BidderName,
		WinningBid: *winner,
	}, nil
}

// betterBid reports whether candidate should beat the incumbent: lower cost
// wins, and on equal cost the higher-confidence bid wins.
func betterBid(candidate, incumbent Bid) bool {
	if candidate.Cost != incumbent.Cost {
		return candidate.Cost < incumbent.Cost
	}
	return candidate.Confidence > incumbent.Confidence
}

// ScoreAnnouncement is the bidder-side inverse of EligibleBidders: an agent
// scores an announced task against its own agent card and produces the bid it
// would submit. It reports ok=false — no bid — when the card cannot cover the
// announcement's RequiredTags, or when the resulting confidence falls below the
// announcement's MinConfidence (a bid the auctioneer would reject anyway).
//
// Confidence measures focus: the fraction of the card's distinct capabilities
// that the task actually demands, so a specialist whose every skill is wanted
// bids at confidence 1, while a generalist carrying irrelevant skills is
// diluted. Cost counts those irrelevant capabilities, so the more focused agent
// bids cheaper. An announcement with no RequiredTags is open to everyone and is
// treated as a perfect fit (confidence 1, cost 0).
func ScoreAnnouncement(bidderName string, card *a2a.AgentCard, ann TaskAnnouncement) (Bid, bool) {
	if card == nil || !cardCoversTags(card, ann.RequiredTags) {
		return Bid{}, false
	}

	confidence, cost := 1.0, 0.0
	if len(ann.RequiredTags) > 0 {
		required := make(map[string]struct{}, len(ann.RequiredTags))
		for _, tag := range ann.RequiredTags {
			required[tag] = struct{}{}
		}

		have := make(map[string]struct{})
		for _, skill := range card.Skills {
			for _, tag := range skill.Tags {
				have[tag] = struct{}{}
			}
		}

		demanded := 0
		for tag := range have {
			if _, ok := required[tag]; ok {
				demanded++
			}
		}
		total := len(have)
		if total == 0 {
			return Bid{}, false
		}
		confidence = float64(demanded) / float64(total)
		cost = float64(total - demanded)
	}

	if confidence < ann.MinConfidence {
		return Bid{}, false
	}

	return Bid{
		TaskID:     ann.TaskID,
		BidderName: bidderName,
		Cost:       cost,
		Confidence: confidence,
	}, true
}

// BidCollector delivers a task announcement to a single candidate agent and
// returns the candidate's raw text response. It is the one seam the auctioneer
// needs from the A2A transport, and is satisfied by *BTAgentClient in
// production and by a fake in tests. An empty response signals that the
// candidate declined to bid.
type BidCollector interface {
	SendTask(ctx context.Context, agentURL, taskText string) (string, error)
}

// Auctioneer opens an auction by fanning a TaskAnnouncement out to candidate
// agents over a BidCollector and gathering their bids. Unreachable, silent,
// malformed, and foreign-task candidates are tolerated: a single bad candidate
// never fails the auction, it is simply omitted from the returned bids.
type Auctioneer struct {
	transport BidCollector

	breakersMu sync.Mutex
	breakers   map[string]*reliability.CircuitBreaker // winner name -> breaker

	// persistent routes every winner breaker lookup through the shared,
	// file-backed winnerBreakers store below instead of the per-instance
	// breakers map above, and flushes it to disk after every dispatch
	// outcome. Set by NewPersistentAuctioneer (the production constructor);
	// left false for NewAuctioneer so ad hoc/test Auctioneers keep cheap,
	// disk-free, per-instance breaker state.
	persistent bool
}

// NewAuctioneer creates an Auctioneer that announces and collects bids over
// the given transport, tracking winner circuit breakers in memory only,
// scoped to this Auctioneer instance.
func NewAuctioneer(transport BidCollector) *Auctioneer {
	return &Auctioneer{transport: transport}
}

// NewPersistentAuctioneer creates an Auctioneer whose winner circuit breakers
// are backed by the shared, process-wide, file-persisted winnerBreakers store
// instead of a fresh per-instance map. Production's AuctionDelegate builds a
// brand-new Auctioneer (and transport) on every single call, so a plain
// NewAuctioneer would start the winner's failure count back at zero on every
// engine tick and the breaker guarding the real production path could never
// actually open. Routing through the shared store lets the failure count
// survive across ticks, and the store's Save/Load round-trip through
// agent.CircuitBreakersFile() so it also survives a process restart.
func NewPersistentAuctioneer(transport BidCollector) *Auctioneer {
	return &Auctioneer{transport: transport, persistent: true}
}

// winnerDispatchRetries/winnerDispatchBaseDelay/winnerDispatchMaxDelay bound
// how hard RunAuction retries a transient winner-dispatch failure before
// giving up on that auction. Non-retryable categories (validation, auth) fail
// immediately regardless of these bounds, so a winner that is simply wrong
// (not merely unreachable) never pays the retry latency.
const (
	winnerDispatchRetries   = 3
	winnerDispatchBaseDelay = 50 * time.Millisecond
	winnerDispatchMaxDelay  = 500 * time.Millisecond
)

// winnerCircuitBreakerThreshold/Cooldown bound how many consecutive
// dispatch failures a single winner may accrue across auctions before
// RunAuction stops dispatching to it at all — the fix for "fires once with no
// fallback": a persistently failing winner must eventually be circuit-broken
// rather than hammered on every subsequent auction.
const (
	winnerCircuitBreakerThreshold = 3
	winnerCircuitBreakerCooldown  = 30 * time.Second
)

// winnerBreaker returns the circuit breaker tracking dispatch failures for
// the named winner, creating it on first use. For a persistent Auctioneer
// (see NewPersistentAuctioneer) the lookup is routed through the shared
// winnerBreakers store, so failures accumulate across every AuctionDelegate
// call, not just repeated RunAuction calls on the same Auctioneer instance.
func (a *Auctioneer) winnerBreaker(name string) *reliability.CircuitBreaker {
	if a.persistent {
		return winnerBreakers.get(name)
	}
	a.breakersMu.Lock()
	defer a.breakersMu.Unlock()
	if a.breakers == nil {
		a.breakers = make(map[string]*reliability.CircuitBreaker)
	}
	cb, ok := a.breakers[name]
	if !ok {
		cb = reliability.NewCircuitBreaker("a2a.auction.winner."+name, winnerCircuitBreakerThreshold, winnerCircuitBreakerCooldown)
		a.breakers[name] = cb
	}
	return cb
}

// persistWinnerBreakerState flushes the shared winner breaker store to
// agent.CircuitBreakersFile() when this Auctioneer is routing through it
// (see NewPersistentAuctioneer). A save failure is logged, not returned,
// matching the scheduler's tolerant persist-after-outcome pattern
// (internal/agent/scheduler.go) — a disk hiccup must never fail the auction
// itself.
func (a *Auctioneer) persistWinnerBreakerState() {
	if !a.persistent {
		return
	}
	if err := winnerBreakers.save(); err != nil {
		slog.Warn("a2a: persist winner circuit breaker state failed", "path", agent.CircuitBreakersFile(), "err", err)
	}
}

// AuctionResult is the outcome of a completed auction: the Award naming the
// winning bidder and the execution Result the winner returned after being
// dispatched the real task text.
type AuctionResult struct {
	Award  Award
	Result string
}

// RunAuction composes the three auction stages end to end: it fans the
// announcement out via CollectBids, picks the winner with a ScoreEvaluator,
// then dispatches the announcement's Description (the real task text) to the
// winning agent's URL over the same transport and returns that execution result
// alongside the Award.
//
// candidates maps a candidate's display name to its A2A base URL — the same map
// handed to CollectBids. The winning agent is located by matching the Award's
// WinnerName back to that map. Only the winner is dispatched the real task; the
// losing candidates are never invoked again. When no bid is eligible to win,
// the evaluator's error (ErrNoEligibleBids) is propagated and nothing is
// dispatched.
func (a *Auctioneer) RunAuction(ctx context.Context, ann TaskAnnouncement, candidates map[string]string) (AuctionResult, error) {
	if ann.Description == "" {
		return AuctionResult{}, fmt.Errorf("a2a: announcement Description is required as the winner's task text")
	}

	bids, err := a.CollectBids(ctx, ann, candidates)
	if err != nil {
		return AuctionResult{}, err
	}

	award, err := ScoreEvaluator{}.Evaluate(ann, bids)
	if err != nil {
		return AuctionResult{}, err
	}

	winnerURL, ok := candidates[award.WinnerName]
	if !ok {
		return AuctionResult{}, fmt.Errorf("a2a: winning bidder %q has no candidate URL", award.WinnerName)
	}

	breaker := a.winnerBreaker(award.WinnerName)
	if !breaker.Allow() {
		return AuctionResult{}, fmt.Errorf("a2a: winner %q circuit breaker open, refusing dispatch", award.WinnerName)
	}

	// Bound the winner dispatch by a deadline derived from the announcement (or a
	// default when it carries none), reusing the same helper the bid fan-out uses,
	// so a winner that hangs cannot block indefinitely on the raw caller context.
	dispatchCtx, cancel := candidateContext(ctx, ann)
	defer cancel()

	// Retry transient (network/timeout) dispatch failures a few times before
	// giving up; non-retryable categories (validation, auth) fail on the first
	// attempt. Either way, every failure feeds the winner's circuit breaker so
	// a persistently failing winner is eventually skipped outright instead of
	// being dispatched to on every subsequent auction.
	policy := &reliability.RetryPolicy{
		MaxRetries: winnerDispatchRetries,
		Base:       winnerDispatchBaseDelay,
		MaxDelay:   winnerDispatchMaxDelay,
		Jitter:     reliability.NoJitter,
	}
	var result string
	dispatchErr := policy.ExecuteContext(dispatchCtx, func() error {
		res, sendErr := a.transport.SendTask(dispatchCtx, winnerURL, ann.Description)
		if sendErr != nil {
			return sendErr
		}
		result = res
		return nil
	})
	if dispatchErr != nil {
		breaker.RecordFailureWithCategory(dispatchErr)
		a.persistWinnerBreakerState()
		return AuctionResult{}, fmt.Errorf("a2a: dispatch task to winner %q: %w", award.WinnerName, dispatchErr)
	}
	breaker.RecordSuccess()
	a.persistWinnerBreakerState()

	return AuctionResult{Award: award, Result: result}, nil
}

// winnerBreakerKeyPrefix namespaces winner breaker entries within
// agent.CircuitBreakersFile() so the store below only ever reads/writes keys
// it owns, leaving the scheduler's per-agent breaker entries in the same file
// untouched.
const winnerBreakerKeyPrefix = "a2a.auction.winner."

// winnerBreakerFileEntry / winnerBreakerFile mirror the on-disk shape
// internal/agent's AgentCircuitBreakerStore.Save/Load already use for
// agent.CircuitBreakersFile() ({"breakers": {name: {status, failures,
// last_failure}}}) — the same shape internal/dashboard's loadCircuitBreakers
// reads for the dashboard's cb_status column — so winner breakers round-trip
// through the exact same file without agent and a2a sharing a Go type.
type winnerBreakerFileEntry struct {
	Status      string `json:"status"`
	Failures    int    `json:"failures"`
	LastFailure string `json:"last_failure,omitempty"`
}

type winnerBreakerFile struct {
	Breakers map[string]winnerBreakerFileEntry `json:"breakers"`
}

// winnerBreakerStore is the process-wide, file-backed store every
// NewPersistentAuctioneer routes its winner breaker lookups through. Keeping
// one breaker instance per winner name alive for the lifetime of the process
// (instead of inside a per-call Auctioneer) is what lets a failure count
// survive across separate AuctionDelegate calls (engine ticks); Save/Load
// against agent.CircuitBreakersFile() is what additionally lets it survive a
// process restart.
type winnerBreakerStore struct {
	mu       sync.Mutex
	breakers map[string]*reliability.CircuitBreaker
	loaded   bool
}

// winnerBreakers is the shared store every production AuctionDelegate call
// reads and writes through.
var winnerBreakers = &winnerBreakerStore{}

// get returns the circuit breaker tracking dispatch failures for the named
// winner, restoring persisted state from agent.CircuitBreakersFile() on first
// use (so a process restart doesn't forget a previously-open breaker) and
// creating a fresh closed breaker when none exists yet, in memory or on disk.
func (s *winnerBreakerStore) get(name string) *reliability.CircuitBreaker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.loaded {
		s.loadLocked()
		s.loaded = true
	}
	if s.breakers == nil {
		s.breakers = make(map[string]*reliability.CircuitBreaker)
	}
	key := winnerBreakerKeyPrefix + name
	cb, ok := s.breakers[key]
	if !ok {
		cb = reliability.NewCircuitBreaker(key, winnerCircuitBreakerThreshold, winnerCircuitBreakerCooldown)
		s.breakers[key] = cb
	}
	return cb
}

// loadLocked restores previously-persisted winner breakers so a process
// restart doesn't forget a known-bad winner. A missing or unreadable file is
// not an error — it's the expected first-boot state before any winner
// breaker has ever tripped. Must be called with s.mu held.
func (s *winnerBreakerStore) loadLocked() {
	data, err := os.ReadFile(agent.CircuitBreakersFile())
	if err != nil {
		return
	}
	var file winnerBreakerFile
	if err := json.Unmarshal(data, &file); err != nil {
		return
	}
	if s.breakers == nil {
		s.breakers = make(map[string]*reliability.CircuitBreaker)
	}
	for key, entry := range file.Breakers {
		if !strings.HasPrefix(key, winnerBreakerKeyPrefix) {
			continue // owned by another writer (e.g. the scheduler's per-agent breakers)
		}
		cb := reliability.NewCircuitBreaker(key, winnerCircuitBreakerThreshold, winnerCircuitBreakerCooldown)
		// reliability.CircuitBreaker exposes no state-setter, so replay the
		// persisted failure count through RecordFailure to reconstruct the
		// same status (closed, or open once it crosses the threshold).
		for i := 0; i < entry.Failures; i++ {
			cb.RecordFailure()
		}
		s.breakers[key] = cb
	}
}

// save persists every tracked winner breaker's state into
// agent.CircuitBreakersFile(), merging with (not clobbering) whatever
// non-winner entries — e.g. the scheduler's per-agent breakers — are already
// in the file.
func (s *winnerBreakerStore) save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	path := agent.CircuitBreakersFile()
	file := winnerBreakerFile{Breakers: map[string]winnerBreakerFileEntry{}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &file) // best-effort merge; a corrupt file is simply overwritten below
	}
	if file.Breakers == nil {
		file.Breakers = map[string]winnerBreakerFileEntry{}
	}
	for key, cb := range s.breakers {
		entry := winnerBreakerFileEntry{
			Status:   cb.State().String(),
			Failures: cb.FailureCount(),
		}
		if t := cb.LastFailureTime(); !t.IsZero() {
			entry.LastFailure = t.Format(time.RFC3339)
		}
		file.Breakers[key] = entry
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal winner circuit breaker state: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create circuit breaker state dir: %w", err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return fmt.Errorf("write winner circuit breaker state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename winner circuit breaker state: %w", err)
	}
	return nil
}

// AuctionCardsFn is the production seam that yields the live A2A card registry
// (agent name → card) production auctions draw their candidate set from. The
// daemon installs it at startup from the registered agents' cards (see
// Server.AuctionCardSource); until then it is nil and AuctionDelegate finds no
// candidates, so the AuctionDelegate action falls back to its delegate tree.
var AuctionCardsFn func() map[string]*a2a.AgentCard

// newAuctionCollector builds the BidCollector transport an Auctioneer fans
// announcements out over. It is a package var so tests can substitute a fake;
// in production it is the real BTAgentClient A2A transport.
var newAuctionCollector = func() BidCollector { return NewBTAgentClient() }

// AuctionDelegate is the production engine.AuctionDelegateFn (installed by
// internal/agentexec's init so every tree-running binary shares it). It builds
// the candidate map (agent name → A2A URL) from the live card registry, runs an
// Auctioneer over the real BTAgentClient transport, and returns the winning
// agent's execution result.
//
// chainState overrides steer candidate selection without recompiling: an
// "auction_candidates" map (name → URL) fully replaces the derived candidate
// set, while "auction_required_tags", "auction_min_confidence", and
// "auction_task_id" shape the announcement (and thus which registered agents are
// eligible to bid).
//
// awarded is false — signalling the caller to fall back to a delegate tree —
// when there are no candidates or no eligible bidder wins; any other
// transport/auction failure is returned as err.
//
// On a win, the resulting AuctionResult.Award is written back into chainState
// under "auction_award" (when chainState is non-nil) so a caller whose return
// signature only carries the bare result string can still attribute the run
// to the winning agent, e.g. for a follow-up History.Record call.
func AuctionDelegate(task string, chainState map[string]any) (string, bool, error) {
	ann := auctionAnnouncement(task, chainState)
	candidates := auctionCandidates(ann, chainState)
	if len(candidates) == 0 {
		return "", false, nil // no candidates → fall back to delegate tree
	}

	res, err := NewPersistentAuctioneer(newAuctionCollector()).RunAuction(context.Background(), ann, candidates)
	if err != nil {
		if errors.Is(err, ErrNoEligibleBids) {
			return "", false, nil // no eligible bidder → fall back to delegate tree
		}
		return "", false, err
	}
	if chainState != nil {
		chainState["auction_award"] = res.Award
	}
	return res.Result, true, nil
}

// auctionAnnouncement builds the TaskAnnouncement for a production auction from
// the task text and optional chainState overrides. The task text becomes the
// announcement Description (the real work dispatched to the winner), while the
// TaskID, RequiredTags, and MinConfidence may be overridden via chainState.
func auctionAnnouncement(task string, chainState map[string]any) TaskAnnouncement {
	ann := TaskAnnouncement{TaskID: "auction", Description: task}
	if id := stateString(chainState, "auction_task_id"); id != "" {
		ann.TaskID = id
	}
	ann.RequiredTags = stateStrings(chainState, "auction_required_tags")
	if mc, ok := stateFloat(chainState, "auction_min_confidence"); ok {
		ann.MinConfidence = mc
	}
	return ann
}

// auctionCandidates resolves the candidate map (agent name → A2A URL) for an
// auction. An explicit "auction_candidates" chainState override wins outright;
// otherwise the map is derived from the live card registry, restricted to the
// agents EligibleBidders reports can cover the announcement's RequiredTags.
func auctionCandidates(ann TaskAnnouncement, chainState map[string]any) map[string]string {
	if override := candidateOverride(chainState); override != nil {
		return override
	}
	if AuctionCardsFn == nil {
		return nil
	}
	cards := AuctionCardsFn()
	if len(cards) == 0 {
		return nil
	}
	trusted := make(map[string]*a2a.AgentCard, len(cards))
	for name, card := range cards {
		if cardSignatureValid(card) {
			trusted[name] = card
		}
	}

	out := make(map[string]string)
	for _, name := range EligibleBidders(trusted, ann) {
		if url := cardURL(trusted[name]); url != "" {
			out[name] = url
		}
	}
	return out
}

// cardURL returns the first non-empty interface URL an agent card advertises, or
// "" when the card exposes no reachable interface.
func cardURL(card *a2a.AgentCard) string {
	if card == nil {
		return ""
	}
	for _, iface := range card.SupportedInterfaces {
		if iface != nil && iface.URL != "" {
			return iface.URL
		}
	}
	return ""
}

// candidateOverride extracts an explicit "auction_candidates" chainState override
// (agent name → A2A URL), tolerating both a typed map[string]string and the
// map[string]any shape a JSON-decoded blackboard produces. It returns nil when
// no usable override is present so the caller falls back to the derived set.
func candidateOverride(chainState map[string]any) map[string]string {
	raw, ok := chainState["auction_candidates"]
	if !ok {
		return nil
	}
	out := map[string]string{}
	switch v := raw.(type) {
	case map[string]string:
		for name, url := range v {
			if name != "" && url != "" {
				out[name] = url
			}
		}
	case map[string]any:
		for name, u := range v {
			if url, ok := u.(string); ok && name != "" && url != "" {
				out[name] = url
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// stateString reads a string chainState value, returning "" when absent or of a
// different type.
func stateString(chainState map[string]any, key string) string {
	if s, ok := chainState[key].(string); ok {
		return s
	}
	return ""
}

// stateStrings reads a string-slice chainState value, tolerating both []string
// and the []any shape a JSON-decoded blackboard produces. It returns nil when
// absent.
func stateStrings(chainState map[string]any, key string) []string {
	switch v := chainState[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// stateFloat reads a numeric chainState value, tolerating the several numeric
// shapes a blackboard may carry (native floats/ints and json.Number). ok is
// false when the key is absent or non-numeric.
func stateFloat(chainState map[string]any, key string) (float64, bool) {
	switch v := chainState[key].(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	}
	return 0, false
}

// candidateContext derives the per-candidate fan-out context from the auction
// context and the announcement's Deadline: it honors an explicit announcement
// deadline when set, and otherwise applies defaultBidDeadline so an unbounded
// announcement still cannot let a hung candidate block the auction.
func candidateContext(ctx context.Context, ann TaskAnnouncement) (context.Context, context.CancelFunc) {
	if !ann.Deadline.IsZero() {
		return context.WithDeadline(ctx, ann.Deadline)
	}
	return context.WithTimeout(ctx, defaultBidDeadline)
}

// CollectBids validates the announcement, fans it out to every candidate
// concurrently, and returns the valid bids sorted by bidder name.
//
// candidates maps a candidate's display name to its A2A base URL. The
// announcement is delivered JSON-encoded as the task text. A candidate's
// response is kept only when it is a well-formed Bid (Validate passes) for the
// announced task; declines (empty response), transport errors, non-JSON
// garbage, structurally invalid bids, and bids for a different task are all
// dropped. An invalid announcement is rejected before any fan-out.
func (a *Auctioneer) CollectBids(ctx context.Context, ann TaskAnnouncement, candidates map[string]string) ([]Bid, error) {
	if err := ann.Validate(); err != nil {
		return nil, fmt.Errorf("a2a: invalid announcement: %w", err)
	}

	payload, err := json.Marshal(ann)
	if err != nil {
		return nil, fmt.Errorf("a2a: marshal announcement: %w", err)
	}
	taskText := string(payload)

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		bids []Bid
	)
	for name, agentURL := range candidates {
		wg.Add(1)
		name, agentURL := name, agentURL
		reliability.SafeGo(fmt.Sprintf("a2a.CollectBids[%s]", name), func() {
			defer wg.Done()

			// Bound this candidate's call by a deadline derived from the
			// announcement (or a default when it carries none), so one hung
			// candidate can never stall the whole fan-out.
			bidCtx, cancel := candidateContext(ctx, ann)
			defer cancel()

			resp, err := a.transport.SendTask(bidCtx, agentURL, taskText)
			if err != nil || resp == "" {
				return // candidate unreachable or declined to bid
			}

			var bid Bid
			if err := json.Unmarshal([]byte(resp), &bid); err != nil {
				return // response was not a bid
			}
			if bid.TaskID != ann.TaskID {
				return // bid for a different task
			}
			// Attribute the bid to the identity the announcement was actually
			// delivered under (the candidates-map key), not the untrusted
			// self-reported BidderName. This keeps the downstream Award's winner
			// lookup resolvable against the candidates map even when a candidate
			// misreports its name.
			bid.BidderName = name
			if err := bid.Validate(); err != nil {
				return // structurally invalid bid
			}

			mu.Lock()
			bids = append(bids, bid)
			mu.Unlock()
		}, nil)
	}
	wg.Wait()

	sort.Slice(bids, func(i, j int) bool {
		return bids[i].BidderName < bids[j].BidderName
	})
	return bids, nil
}
