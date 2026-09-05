package agent

import "context"

// RunAgent executes an agent through RunOnce with registry-first tree resolution.
func RunAgent(ctx context.Context, d *RunDeps, agentName, task, treeOverride string, opts RunOptions) (outcome, output string, res *RunResult, err error) {
	if d == nil {
		return "failure", "", nil, errRunnerNotConfigured
	}
	if opts.DisplayName == "" {
		opts.DisplayName = agentName
	}

	runTarget := d.ResolveRunTarget(agentName)
	if treeOverride != "" {
		runTarget = treeOverride
	}

	res, err = d.RunOnce(ctx, runTarget, task, opts)
	if res != nil {
		outcome = res.Outcome
		output = res.Output
		if outcome == "" {
			outcome = "failure"
		}
	}
	if err != nil && outcome == "" {
		outcome = "failure"
	}
	return outcome, output, res, err
}

var errRunnerNotConfigured = runnerError("agent runner not configured (LLM or stores unavailable)")

type runnerError string

func (e runnerError) Error() string { return string(e) }
