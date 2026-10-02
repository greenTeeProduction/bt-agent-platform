#!/usr/bin/env python3
"""Regression cases for the architecture drift gate (no third-party packages)."""

import importlib.util
from pathlib import Path
import tempfile
import unittest

spec = importlib.util.spec_from_file_location(
    "check_arc42", Path(__file__).with_name("check-arc42.py"))
checker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(checker)


class ArchitectureChecks(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for name in ("docs/arc42", "internal/engine", "cmd/bt-agent"):
            (self.root / name).mkdir(parents=True)
        self.write("01-introduction-goals.md",
                   "| Q1 | **Correctness** | See QS1. |\n")
        self.write("04-solution-strategy.md", "| Q1 | Validate before execution |\n")
        self.write("05-building-blocks.md",
                   "| `internal/engine` | Execution |\n"
                   "### Entrypoints\n\n| `bt-agent` | MCP |\n")
        self.write("10-quality.md", "| QS1 | Q1 | Task | Reject invalid input | Tested |\n")
        self.write("11-risks-debt.md", "| R1 | High/open | Missing evidence | Measure |\n")
        self.write("09-decisions.md",
                   "| ADR-001 | [Trees](#adr-001) | Accepted | 2026-09-16 |\n"
                   '<a id="adr-001"></a>\n\n## ADR-001: Trees\n'
                   "**Status:** Accepted\n")
        self.write("README.md",
                   "[Goals](01-introduction-goals.md)\n"
                   "[Decision](09-decisions.md#adr-001)\n")

    def write(self, name, content):
        (self.root / "docs/arc42" / name).write_text(content)

    def append(self, name, content):
        path = self.root / "docs/arc42" / name
        path.write_text(path.read_text() + content)

    def errors(self):
        return "\n".join(checker.check(self.root))

    def test_valid_architecture(self):
        self.assertEqual(self.errors(), "")

    def test_missing_evidence_file_and_anchor(self):
        self.append("README.md", "[Evidence](../../internal/engine/missing.go)\n"
                    "[Wrong](09-decisions.md#absent)\n")
        self.assertIn("missing link target", self.errors())
        self.assertIn("missing anchor", self.errors())

    def test_heading_code_and_example_links(self):
        self.append("01-introduction-goals.md", "## The `engine` boundary\n")
        self.append("README.md", "[Heading](01-introduction-goals.md#the-engine-boundary)\n"
                    "`[example](missing-inline.md)`\n"
                    "```markdown\n[example](missing-fenced.md)\n```\n")
        self.assertEqual(self.errors(), "")

    def test_new_package_and_removed_command_require_documentation(self):
        (self.root / "internal/new_component").mkdir()
        (self.root / "cmd/bt-agent").rmdir()
        self.assertIn("undocumented internal/new_component", self.errors())
        self.assertIn("nonexistent cmd/bt-agent", self.errors())

    def test_api_inventory_and_operator_links_are_checked(self):
        reference = self.root / "docs/API_REFERENCE.md"
        reference.write_text("[`removed`](#package-removed)\n\n## Package: removed\n")
        self.assertIn("API_REFERENCE: nonexistent internal/removed", self.errors())
        self.assertIn("API_REFERENCE: undocumented internal/engine", self.errors())
        reference.write_text("[`engine`](#package-engine)\n\n## Package: engine\n")
        self.assertEqual(self.errors(), "")
        (self.root / "docs/TROUBLESHOOTING.md").write_text("[ADR](adr/INDEX.md)\n")
        self.assertIn("missing link target adr/INDEX.md", self.errors())

    def test_goal_needs_strategy_and_scenario(self):
        self.append("01-introduction-goals.md", "| Q2 | **Reliability** | Recover |\n")
        self.assertIn("Q2: no quality scenario", self.errors())
        self.assertIn("Q2: missing strategy mapping", self.errors())

    def test_prose_mention_does_not_replace_strategy_mapping(self):
        self.write("04-solution-strategy.md", "We care about Q1.\n")
        self.assertIn("Q1: missing strategy mapping", self.errors())

    def test_scenario_needs_existing_goal_and_current_references_must_resolve(self):
        self.write("10-quality.md", "| QS1 | Q9 | Context | Response | Target |\n")
        self.append("11-risks-debt.md", "See QS2 and ADR-002.\n")
        errors = self.errors()
        self.assertIn("QS1: unknown goal Q9", errors)
        self.assertIn("unknown reference QS2", errors)
        self.assertIn("unknown reference ADR-002", errors)

    def test_duplicate_ids_are_rejected(self):
        self.append("10-quality.md", "| QS1 | Q1 | Again | Response | Target |\n")
        self.append("09-decisions.md",
                    '<a id="adr-001"></a>\n\n## ADR-001: Duplicate\n'
                    "**Status:** Accepted\n")
        self.assertIn("duplicate QS1", self.errors())
        self.assertIn("duplicate ADR-001", self.errors())

    def test_missing_middle_adr_index_row_and_wrong_destination(self):
        self.append("09-decisions.md",
                    '<a id="adr-002"></a>\n\n## ADR-002: Other\n'
                    "**Status:** Accepted\n"
                    "| ADR-003 | [Last](#adr-001) | Accepted | 2026-09-16 |\n"
                    '<a id="adr-003"></a>\n\n## ADR-003: Last\n'
                    "**Status:** Accepted\n")
        errors = self.errors()
        self.assertIn("ADR-002: index/record count mismatch", errors)
        self.assertIn("ADR index must link each ID to its own", errors)

    def test_only_named_historical_adr_duplicate_is_allowed(self):
        for number in range(2, 24):
            num = f"{number:03d}"
            self.append("09-decisions.md",
                        f"| ADR-{num} | [Decision](#adr-{num}) | Accepted | date |\n"
                        f'<a id="adr-{num}"></a>\n\n## ADR-{num}: Decision\n'
                        "**Status:** Accepted\n")
        for suffix in ("composition", "experience-bank"):
            self.append("09-decisions.md",
                        f"| ADR-024 | [Legacy](#adr-024-{suffix}) | Accepted | date |\n"
                        f'<a id="adr-024-{suffix}"></a>\n\n## ADR-024: Legacy\n'
                        "**Status:** Accepted\n")
        self.assertEqual(self.errors(), "")
        path = self.root / "docs/arc42/09-decisions.md"
        path.write_text(path.read_text().replace("adr-024-experience-bank", "adr-024"))
        self.assertIn("missing adjacent stable anchor", self.errors())


if __name__ == "__main__":
    unittest.main()
