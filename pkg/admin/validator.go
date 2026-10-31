package admin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rgbyte/lattice/pkg/types"
)

const (
	IssueFailClosedNode    = "ISSUE_FAIL_CLOSED_NODE"
	IssueSupersessionCycle = "ISSUE_SUPERSESSION_CYCLE"
	IssueDanglingEndpoint  = "ISSUE_DANGLING_ENDPOINT"
)

// ValidationIssue describes a single structural or security anomaly discovered in the Context Graph.
type ValidationIssue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"` // "error" or "warning"
	EntityID string `json:"entity_id"`
	Message  string `json:"message"`
}

// ValidationReport summarizes a full Context Graph integrity audit.
type ValidationReport struct {
	Valid        bool              `json:"valid"`
	ErrorCount   int               `json:"error_count"`
	WarningCount int               `json:"warning_count"`
	Issues       []ValidationIssue `json:"issues"`
	CheckedAt    time.Time         `json:"checked_at"`
}

// ACLTargetDecision captures the detailed ACL evaluation outcome for a single target resource.
type ACLTargetDecision struct {
	TargetID    string   `json:"target_id"`
	Kind        string   `json:"kind"`
	Allowed     bool     `json:"allowed"`
	MatchedRule string   `json:"matched_rule,omitempty"`
	Permissions []string `json:"permissions"`
	Reason      string   `json:"reason"`
}

// ACLSimulationReport summarizes a multi-target access control simulation for a PrincipalSet.
type ACLSimulationReport struct {
	Principal types.PrincipalSet  `json:"principal"`
	Decisions []ACLTargetDecision `json:"decisions"`
	CheckedAt time.Time           `json:"checked_at"`
}

// ValidateIntegrity audits the live Context Graph for fail-closed unreachable nodes,
// circular document supersession chains, and dangling edge endpoints.
func (s *Service) ValidateIntegrity(ctx context.Context) (*ValidationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	nodes := s.eng.GraphIndex().AllNodes()
	edges := s.eng.GraphIndex().AllEdges()

	nodeMap := make(map[string]types.Node, len(nodes))
	for _, n := range nodes {
		nodeMap[n.ID] = n
	}

	var issues []ValidationIssue
	errorCount := 0
	warningCount := 0

	addIssue := func(code, severity, entityID, msg string) {
		if severity == "error" {
			errorCount++
		} else {
			warningCount++
		}
		issues = append(issues, ValidationIssue{
			Code:     code,
			Severity: severity,
			EntityID: entityID,
			Message:  msg,
		})
	}

	// 1. Check for fail-closed nodes with empty Permissions
	for _, n := range nodes {
		if len(n.Permissions) == 0 {
			addIssue(
				IssueFailClosedNode,
				"warning",
				n.ID,
				fmt.Sprintf("node %q (%s) has empty permissions and is unreachable under fail-closed ACL policy", n.ID, n.Kind),
			)
		}
	}

	// 2. Check for dangling edge endpoints & build supersession adjacency list
	supersedesNext := make(map[string][]string)
	for _, e := range edges {
		if _, ok := nodeMap[e.SourceID]; !ok {
			addIssue(
				IssueDanglingEndpoint,
				"warning",
				e.ID,
				fmt.Sprintf("edge %q source endpoint %q is missing from GraphIndex", e.ID, e.SourceID),
			)
		}
		if _, ok := nodeMap[e.TargetID]; !ok {
			addIssue(
				IssueDanglingEndpoint,
				"warning",
				e.ID,
				fmt.Sprintf("edge %q target endpoint %q is missing from GraphIndex", e.ID, e.TargetID),
			)
		}
		if e.Relation == types.RelSupersedes {
			supersedesNext[e.SourceID] = append(supersedesNext[e.SourceID], e.TargetID)
		}
	}

	// 3. Detect circular RelSupersedes chains (A -[SUPERSEDES]-> B -[SUPERSEDES]-> A)
	// State: 0 = unvisited, 1 = visiting (on current recursion stack), 2 = visited
	state := make(map[string]int, len(supersedesNext))
	reportedCycleNodes := make(map[string]bool)

	var dfs func(u string, stack []string)
	dfs = func(u string, stack []string) {
		state[u] = 1
		stack = append(stack, u)
		for _, v := range supersedesNext[u] {
			if state[v] == 1 {
				if !reportedCycleNodes[v] {
					reportedCycleNodes[v] = true
					cyclePath := append(append([]string(nil), stack...), v)
					addIssue(
						IssueSupersessionCycle,
						"error",
						v,
						fmt.Sprintf("circular SUPERSEDES chain detected: %s", strings.Join(cyclePath, " -> ")),
					)
				}
			} else if state[v] == 0 {
				dfs(v, stack)
			}
		}
		state[u] = 2
	}

	for _, n := range nodes {
		if state[n.ID] == 0 && len(supersedesNext[n.ID]) > 0 {
			dfs(n.ID, nil)
		}
	}

	return &ValidationReport{
		Valid:        errorCount == 0,
		ErrorCount:   errorCount,
		WarningCount: warningCount,
		Issues:       issues,
		CheckedAt:    time.Now().UTC(),
	}, nil
}

// SimulateAccess evaluates a PrincipalSet against each target node or document ID and explains
// the exact ACL rule that granted access or why fail-closed denial occurred.
func (s *Service) SimulateAccess(
	ctx context.Context,
	principal types.PrincipalSet,
	targetIDs []string,
) (*ACLSimulationReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	decisions := make([]ACLTargetDecision, 0, len(targetIDs))
	for _, rawID := range targetIDs {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}

		var perms []string
		var kind string
		found := false

		if doc, err := s.eng.Storage().GetDocument(ctx, id); err == nil {
			perms = doc.Permissions
			kind = string(types.NodeDocument)
			found = true
		} else if node, ok := s.eng.GraphIndex().GetNode(id); ok {
			perms = node.Permissions
			kind = string(node.Kind)
			found = true
		}

		if !found {
			decisions = append(decisions, ACLTargetDecision{
				TargetID: id,
				Kind:     "unknown",
				Allowed:  false,
				Reason:   "target resource does not exist (fail-closed)",
			})
			continue
		}

		matched, reason, allowed := principal.MatchRuleDetailed(perms)
		decisions = append(decisions, ACLTargetDecision{
			TargetID:    id,
			Kind:        kind,
			Allowed:     allowed,
			MatchedRule: matched,
			Permissions: perms,
			Reason:      reason,
		})
	}

	return &ACLSimulationReport{
		Principal: principal,
		Decisions: decisions,
		CheckedAt: time.Now().UTC(),
	}, nil
}
