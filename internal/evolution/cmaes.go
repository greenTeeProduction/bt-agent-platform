package evolution

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// ─── CMA-ES Parameter Tuner ────────────────────────────────────────────────
//
// Covariance Matrix Adaptation Evolution Strategy for continuous BT parameter
// optimization. After GA establishes topology, CMA-ES fine-tunes node-level
// numeric parameters (thresholds, timeouts, retry counts, learning rates).
//
// Algorithm (simplified λ,μ-CMA-ES):
//   1. Sample λ candidate solutions from N(m, σ²C)
//   2. Evaluate fitness for each candidate
//   3. Select μ best for recombination (μ = λ/2)
//   4. Update mean via weighted recombination
//   5. Update evolution paths (p_sigma, p_c)
//   6. Update covariance matrix C
//   7. Update step size σ
//   8. Repeat until convergence or max generations
//
// Research sources: daily/2026-05-27, daily/2026-05-28, daily/2026-05-29

// TunableParam represents a continuous parameter that CMA-ES can optimize.
type TunableParam struct {
	Name      string  `json:"name"`
	Lower     float64 `json:"lower"`
	Upper     float64 `json:"upper"`
	InitValue float64 `json:"init_value"`
	// NodePath is a dot-separated path identifying the tree node (e.g. "children.0.children.2")
	// Empty means global parameter not tied to a specific node position.
	NodePath string `json:"node_path,omitempty"`
	// MetaKey is the metadata key this parameter maps to.
	MetaKey string `json:"meta_key,omitempty"`
}

// CMAESOptimizer implements covariance matrix adaptation evolution strategy.
type CMAESOptimizer struct {
	// Population parameters
	PopulationSize int     `json:"population_size"`
	InitialSigma   float64 `json:"initial_sigma"`   // initial step size
	MaxGenerations int     `json:"max_generations"` // max iterations
	TargetFitness  float64 `json:"target_fitness"`  // early stop threshold

	// Internal state (evolved)
	Mean     []float64      `json:"mean"`     // current distribution mean
	Sigma    float64        `json:"sigma"`    // step size
	C        [][]float64    `json:"-"`        // covariance matrix (n×n)
	PSigma   []float64      `json:"-"`        // isotropic evolution path
	PC       []float64      `json:"-"`        // anisotropic evolution path
	Params   []TunableParam `json:"params"`   // parameter definitions
	BestFit  float64        `json:"best_fit"` // best fitness seen
	BestSol  []float64      `json:"best_sol"` // best solution seen
	GenCount int            `json:"gen_count"`
}

// NewCMAESOptimizer creates a CMA-ES optimizer with sensible defaults.
func NewCMAESOptimizer() *CMAESOptimizer {
	return &CMAESOptimizer{
		PopulationSize: 10,
		InitialSigma:   0.3,
		MaxGenerations: 20,
		TargetFitness:  95.0,
		Sigma:          0.3,
	}
}

// ─── Parameter Extraction ──────────────────────────────────────────────────

// ExtractParameters gathers all tunable numeric parameters from a tree.
// Returns a flat list of TunableParam with path/key annotations.
func ExtractParameters(tree *SerializableNode) []TunableParam {
	if tree == nil {
		return nil
	}
	var params []TunableParam
	collectParams(tree, "", &params)
	return params
}

func collectParams(node *SerializableNode, path string, params *[]TunableParam) {
	if node.Metadata != nil {
		// TimeoutMs
		if _, ok := node.Metadata["timeout_ms"]; ok {
			*params = append(*params, TunableParam{
				Name:      node.Name + ".timeout_ms",
				Lower:     100,
				Upper:     60000,
				InitValue: 30000,
				NodePath:  path,
				MetaKey:   "timeout_ms",
			})
		}
		// Threshold
		if _, ok := node.Metadata["threshold"]; ok {
			*params = append(*params, TunableParam{
				Name:      node.Name + ".threshold",
				Lower:     0,
				Upper:     1,
				InitValue: 0.5,
				NodePath:  path,
				MetaKey:   "threshold",
			})
		}
		// MaxRetries
		if node.MaxRetries > 0 {
			*params = append(*params, TunableParam{
				Name:      node.Name + ".max_retries",
				Lower:     0,
				Upper:     10,
				InitValue: float64(node.MaxRetries),
				NodePath:  path,
				MetaKey:   "max_retries",
			})
		}
	}

	for i := range node.Children {
		childPath := path
		if childPath == "" {
			childPath = "children." + itoa(i)
		} else {
			childPath = childPath + ".children." + itoa(i)
		}
		collectParams(&node.Children[i], childPath, params)
	}
}

// ApplyParameters writes optimized parameter values back into a tree.
func ApplyParameters(tree *SerializableNode, params []TunableParam, solution []float64) {
	if tree == nil || len(params) == 0 || len(solution) == 0 {
		return
	}
	for i, p := range params {
		if i >= len(solution) {
			break
		}
		val := solution[i]
		// Clamp to bounds
		if val < p.Lower {
			val = p.Lower
		}
		if val > p.Upper {
			val = p.Upper
		}
		setParamByPath(tree, p.NodePath, p.MetaKey, val)
	}
}

func setParamByPath(node *SerializableNode, path, metaKey string, val float64) {
	if node == nil {
		return
	}
	target := node
	if path != "" {
		target = navigateToPath(node, path)
	}
	if target == nil {
		return
	}
	switch metaKey {
	case "timeout_ms":
		target.TimeoutMs = int64(math.Round(val))
	case "max_retries":
		target.MaxRetries = int(math.Round(val))
	default:
		if target.Metadata == nil {
			target.Metadata = make(map[string]any)
		}
		target.Metadata[metaKey] = val
	}
}

func navigateToPath(node *SerializableNode, path string) *SerializableNode {
	// Simple path parser: "children.0.children.2" etc.
	if path == "" || node == nil {
		return node
	}
	current := node
	parts := splitPath(path)
	for _, part := range parts {
		if part == "" {
			continue
		}
		idx, err := parseInt(part)
		if err != nil || idx < 0 || idx >= len(current.Children) {
			return nil
		}
		current = &current.Children[idx]
	}
	return current
}

func splitPath(path string) []string {
	var parts []string
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			if start < i {
				parts = append(parts, path[start:i])
			}
			start = i + 1
		}
	}
	return parts
}

func parseInt(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not a number: %s", s)
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

// ─── Core CMA-ES ───────────────────────────────────────────────────────────

// Initialize sets up internal state from parameter definitions.
func (cma *CMAESOptimizer) Initialize(params []TunableParam) {
	n := len(params)
	cma.Params = params
	cma.Mean = make([]float64, n)
	cma.Sigma = cma.InitialSigma

	// Initialize mean vector from param initial values (normalized 0-1)
	for i, p := range params {
		range_ := p.Upper - p.Lower
		if range_ <= 0 {
			range_ = 1
		}
		cma.Mean[i] = (p.InitValue - p.Lower) / range_
		// Clamp to [0, 1]
		if cma.Mean[i] < 0 {
			cma.Mean[i] = 0
		}
		if cma.Mean[i] > 1 {
			cma.Mean[i] = 1
		}
	}

	// Initialize covariance matrix C = I
	cma.C = make([][]float64, n)
	for i := 0; i < n; i++ {
		cma.C[i] = make([]float64, n)
		cma.C[i][i] = 1.0
	}

	// Initialize evolution paths
	cma.PSigma = make([]float64, n)
	cma.PC = make([]float64, n)

	// Best solution tracking
	cma.BestSol = make([]float64, n)
	copy(cma.BestSol, cma.Mean)
	cma.BestFit = -1e9
}

// Optimize runs the CMA-ES algorithm for the given number of generations.
// fitnessFn takes a solution vector (normalized 0-1) and returns a score (higher is better).
// Returns the best solution vector found (denormalized).
func (cma *CMAESOptimizer) Optimize(
	params []TunableParam,
	fitnessFn func(solution []float64) float64,
) []float64 {
	cma.Initialize(params)
	n := len(params)

	// CMA-ES hyperparameters
	lambda := cma.PopulationSize
	if lambda < 5 {
		lambda = 5
	}
	mu := lambda / 2 // number of parents
	weights := make([]float64, mu)
	for i := 0; i < mu; i++ {
		weights[i] = math.Log(float64(mu)+0.5) - math.Log(float64(i)+1)
	}
	// Normalize weights
	sumW := 0.0
	for _, w := range weights {
		sumW += w
	}
	for i := range weights {
		weights[i] /= sumW
	}

	// Variance-effective selection mass
	mueff := 0.0
	for _, w := range weights {
		mueff += w * w
	}
	mueff = 1.0 / mueff

	// Learning rates
	cc := (4.0 + mueff/float64(n)) / (float64(n) + 4.0 + 2.0*mueff/float64(n))
	cs := (mueff + 2.0) / (float64(n) + mueff + 5.0)
	c1 := 2.0 / ((float64(n)+1.3)*(float64(n)+1.3) + mueff)
	cmu := math.Min(1-c1, 2.0*(mueff-2.0+1.0/mueff)/((float64(n)+2.0)*(float64(n)+2.0)+mueff))
	damps := 1.0 + 2.0*math.Max(0, math.Sqrt((mueff-1.0)/(float64(n)+1.0))-1.0) + cs

	// Expectation of ||N(0,I)|| (chi-squared distribution)
	chiN := math.Sqrt(float64(n)) * (1.0 - 1.0/(4.0*float64(n)) + 1.0/(21.0*float64(n)*float64(n)))

	for gen := 0; gen < cma.MaxGenerations; gen++ {
		cma.GenCount++

		// 1. Sample λ candidates
		candidates := make([][]float64, lambda)
		fitnesses := make([]float64, lambda)
		for k := 0; k < lambda; k++ {
			// Sample from N(mean, sigma²*C)
			z := sampleStdNormal(n)          // standard normal vector
			zC := multiplyCholesky(cma.C, z) // sqrt(C) * z
			candidate := make([]float64, n)
			for j := 0; j < n; j++ {
				candidate[j] = cma.Mean[j] + cma.Sigma*zC[j]
				// Clamp to [0, 1]
				if candidate[j] < 0 {
					candidate[j] = 0
				}
				if candidate[j] > 1 {
					candidate[j] = 1
				}
			}
			candidates[k] = candidate
			fitnesses[k] = fitnessFn(candidate)

			// Track best
			if fitnesses[k] > cma.BestFit {
				cma.BestFit = fitnesses[k]
				copy(cma.BestSol, candidate)
			}
		}

		// 2. Sort by fitness descending
		idx := make([]int, lambda)
		for i := range idx {
			idx[i] = i
		}
		sort.Slice(idx, func(i, j int) bool {
			return fitnesses[idx[i]] > fitnesses[idx[j]]
		})

		// 3. Early stop if target reached
		if fitnesses[idx[0]] >= cma.TargetFitness {
			break
		}

		// 4. Weighted recombination → new mean
		newMean := make([]float64, n)
		for j := 0; j < mu; j++ {
			for k := 0; k < n; k++ {
				newMean[k] += weights[j] * candidates[idx[j]][k]
			}
		}

		// 5. Update evolution paths
		meanDiff := make([]float64, n)
		for j := 0; j < n; j++ {
			meanDiff[j] = newMean[j] - cma.Mean[j]
		}

		// p_sigma = (1-cs)*p_sigma + sqrt(cs*(2-cs)*mueff) * C^(-1/2) * meanDiff / sigma
		invSqrtC := invertCholesky(cma.C)
		z := make([]float64, n)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				z[i] += invSqrtC[i][j] * meanDiff[j]
			}
		}
		csFactor := math.Sqrt(cs * (2 - cs) * mueff)
		for j := 0; j < n; j++ {
			cma.PSigma[j] = (1-cs)*cma.PSigma[j] + csFactor*z[j]/cma.Sigma
		}

		// p_c = (1-cc)*p_c + h_sigma * sqrt(cc*(2-cc)*mueff) * meanDiff / sigma
		hSigma := 0.0
		psNorm := 0.0
		for j := 0; j < n; j++ {
			psNorm += cma.PSigma[j] * cma.PSigma[j]
		}
		psNorm = math.Sqrt(psNorm)
		if psNorm/math.Sqrt(1.0-math.Pow(1-cs, 2.0*float64(gen+1)))/chiN < 1.4+2.0/(float64(n)+1.0) {
			hSigma = 1
		}

		ccFactor := math.Sqrt(cc * (2 - cc) * mueff)
		for j := 0; j < n; j++ {
			cma.PC[j] = (1-cc)*cma.PC[j] + hSigma*ccFactor*meanDiff[j]/cma.Sigma
		}

		// 6. Update covariance matrix
		deltaH := (1 - hSigma) * cc * (2 - cc)
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				// Rank-one update
				cma.C[i][j] = (1-c1-cmu)*cma.C[i][j] + c1*(cma.PC[i]*cma.PC[j]+deltaH*cma.C[i][j])
				// Rank-mu update: sum over selected parents
				for k := 0; k < mu; k++ {
					yk := make([]float64, n)
					for l := 0; l < n; l++ {
						yk[l] = (candidates[idx[k]][l] - cma.Mean[l]) / cma.Sigma
					}
					cma.C[i][j] += cmu * weights[k] * yk[i] * yk[j]
				}
			}
		}

		// 7. Update step size σ
		psNormFactor := psNorm / chiN
		cma.Sigma *= math.Exp((cs / damps) * (psNormFactor - 1.0))

		// Clamp sigma to prevent explosion
		if cma.Sigma > 10.0 {
			cma.Sigma = 10.0
		}
		if cma.Sigma < 1e-6 {
			cma.Sigma = 1e-6
		}

		// Update mean
		cma.Mean = newMean

		// Ensure covariance matrix symmetry
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				avg := (cma.C[i][j] + cma.C[j][i]) / 2.0
				cma.C[i][j] = avg
				cma.C[j][i] = avg
			}
		}
	}

	// Denormalize best solution
	result := make([]float64, n)
	for i, p := range params {
		range_ := p.Upper - p.Lower
		result[i] = p.Lower + cma.BestSol[i]*range_
		if result[i] < p.Lower {
			result[i] = p.Lower
		}
		if result[i] > p.Upper {
			result[i] = p.Upper
		}
	}
	return result
}

// ─── CMA-ES Utilities ──────────────────────────────────────────────────────

// sampleStdNormal returns a vector of n independent N(0,1) samples.
func sampleStdNormal(n int) []float64 {
	v := make([]float64, n)
	for i := range v {
		// Box-Muller transform
		u1 := rand.Float64()
		u2 := rand.Float64()
		v[i] = math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	}
	return v
}

// multiplyCholesky computes L * x where L is the lower Cholesky factor of C.
// Returns sqrt(C) * x = L * x.
func multiplyCholesky(C [][]float64, x []float64) []float64 {
	n := len(x)
	L := cholesky(C)
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		for j := 0; j <= i; j++ {
			result[i] += L[i][j] * x[j]
		}
	}
	return result
}

// cholesky computes the lower triangular Cholesky decomposition of positive-definite C.
// Returns L such that C = L * L^T.
func cholesky(C [][]float64) [][]float64 {
	n := len(C)
	L := make([][]float64, n)
	for i := 0; i < n; i++ {
		L[i] = make([]float64, n)
		for j := 0; j <= i; j++ {
			s := 0.0
			for k := 0; k < j; k++ {
				s += L[i][k] * L[j][k]
			}
			if i == j {
				L[i][j] = math.Sqrt(C[i][i] - s)
			} else {
				L[i][j] = (C[i][j] - s) / L[j][j]
			}
		}
	}
	return L
}

// invertCholesky computes the inverse of the lower Cholesky factor L.
// Used for C^(-1/2) = L^(-T) * L^(-1), but we compute L^(-1) directly.
func invertCholesky(C [][]float64) [][]float64 {
	n := len(C)
	L := cholesky(C)

	// Forward substitution to invert L
	Linv := make([][]float64, n)
	for i := 0; i < n; i++ {
		Linv[i] = make([]float64, n)
		Linv[i][i] = 1.0 / L[i][i]
		for j := 0; j < i; j++ {
			s := 0.0
			for k := j; k < i; k++ {
				s += L[i][k] * Linv[k][j]
			}
			Linv[i][j] = -s / L[i][i]
		}
	}

	// Compute L^(-T) * L^(-1) = (L*L^T)^(-1) = C^(-1)
	// But we need C^(-1/2) = L^(-T), so just transpose L^(-1)
	// Actually for C^(-1/2) * x we want L^(-T) * x, so return Linv^T
	result := make([][]float64, n)
	for i := 0; i < n; i++ {
		result[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			result[i][j] = Linv[j][i] // transpose
		}
	}
	return result
}

// itoa converts an integer to string (avoiding strconv import for this package).
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	if neg {
		s = "-" + s
	}
	return s
}
