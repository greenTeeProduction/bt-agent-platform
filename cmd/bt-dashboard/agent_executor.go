package main

import "github.com/nico/go-bt-evolve/internal/dashboard"

func newAgentExecutor() *dashboard.AgentExecutor {
	e := dashboard.NewAgentExecutor()
	e.Runner = dashAgentRunner
	return e
}
