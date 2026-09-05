package engine

// arc42 section manifest (spec 2026-07-16-arc42-docs-consolidation-design.md):
// the single Go source of truth for the per-section documentation sync layer.
// scripts/check-doc-drift.sh embeds a bash mirror of RequiredHeadings — keep
// the two in lockstep (TestArc42SectionsAgainstRealRepoDocs pins this side).

const arc42DocsDir = "docs/arc42"

// Arc42Section describes one arc42 section file for the sync layer.
type Arc42Section struct {
	Num              int
	File             string
	Title            string
	RequiredHeadings []string
}

var arc42Sections = []Arc42Section{
	{1, "01-introduction-goals.md", "Introduction and Goals", []string{"# 1. Introduction and Goals", "## 1.1 Requirements Overview", "## 1.2 Quality Goals", "## 1.3 Stakeholders"}},
	{2, "02-constraints.md", "Architecture Constraints", []string{"# 2. Architecture Constraints", "## Technical Constraints", "## Organizational Constraints", "## Conventions"}},
	{3, "03-context-scope.md", "Context and Scope", []string{"# 3. Context and Scope", "## 3.1 Business Context", "## 3.2 Technical Context"}},
	{4, "04-solution-strategy.md", "Solution Strategy", []string{"# 4. Solution Strategy", "## Quality Goals → Solution Approaches", "## Key Technology Decisions"}},
	{5, "05-building-blocks.md", "Building Block View", []string{"# 5. Building Block View", "## 5.1 Whitebox Overall System"}},
	{6, "06-runtime-view.md", "Runtime View", []string{"# 6. Runtime View"}},
	{7, "07-deployment.md", "Deployment View", []string{"# 7. Deployment View", "## 7.1 Infrastructure Level 1"}},
	{8, "08-crosscutting-concepts.md", "Crosscutting Concepts", []string{"# 8. Crosscutting Concepts", "## 8.1"}},
	{9, "09-decisions.md", "Architecture Decisions", []string{"# 9. Architecture Decisions"}},
	{10, "10-quality.md", "Quality Requirements", []string{"# 10. Quality Requirements", "## 10.1 Quality Tree", "## 10.2 Quality Scenarios"}},
	{11, "11-risks-debt.md", "Risks and Technical Debt", []string{"# 11. Risks and Technical Debt"}},
	{12, "12-glossary.md", "Glossary", []string{"# 12. Glossary"}},
}
