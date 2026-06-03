# 1. Introduction and Goals

## 1.1 Requirements Overview

go-bt-evolve is a behavior-tree-driven AI agent platform.

## 1.2 Quality Goals

| Goal | Scenario |
|------|----------|
| Correctness | Trees route tasks to correct domain paths |
| Evolvability | 6 evolution algorithms continuously improve trees |
| Reliability | Panic recovery, circuit breakers, retry with DLQ |

## 1.3 Stakeholders

| Role | Expectations |
|------|-------------|
| Nico | Platform architect and developer |
| Hermes Agent | Automated operator via cron jobs |
| Dashboard Users | Visual introspection of agents, trees, tasks |