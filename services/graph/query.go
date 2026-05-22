package main

import (
	"sort"
	"time"
)

func (s *Server) getGraph(projectID, env string) InfraGraph {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return buildGraph(projectID, env, s.nodes, s.edges, time.Now())
}

func buildGraph(projectID, env string, nodes map[string]*GraphNode, edges map[string]*GraphEdge, snapshot time.Time) InfraGraph {
	g := InfraGraph{
		ProjectID:   projectID,
		Environment: env,
		Snapshot:    snapshot,
	}
	for _, n := range nodes {
		if (projectID == "" || n.ProjectID == projectID) && (env == "" || n.Environment == env) {
			g.Nodes = append(g.Nodes, *n)
		}
	}
	nodeSet := make(map[string]bool, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSet[n.ID] = true
	}
	for _, e := range edges {
		if nodeSet[e.From] && nodeSet[e.To] {
			g.Edges = append(g.Edges, *e)
		}
	}
	return g
}

func (s *Server) findNode(idOrName string) *GraphNode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if n, ok := s.nodes[idOrName]; ok {
		return n
	}
	for _, n := range s.nodes {
		if n.Name == idOrName {
			return n
		}
	}
	return nil
}

func (s *Server) blastRadius(idOrName string) BlastRadiusResult {
	node := s.findNode(idOrName)
	if node == nil {
		return BlastRadiusResult{NodeID: idOrName, Severity: "low"}
	}

	type entry struct {
		id    string
		depth int
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	visited := map[string]int{node.ID: 0}
	queue := []entry{{node.ID, 0}}
	var affected []AffectedNode

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, e := range s.edges {
			if e.To != cur.id {
				continue
			}
			if _, seen := visited[e.From]; seen {
				continue
			}
			depth := cur.depth + 1
			visited[e.From] = depth
			queue = append(queue, entry{e.From, depth})
			a := AffectedNode{ID: e.From, Depth: depth}
			if n, ok := s.nodes[e.From]; ok {
				a.Name = n.Name
				a.Type = string(n.Type)
			}
			affected = append(affected, a)
		}
	}

	sort.Slice(affected, func(i, j int) bool {
		if affected[i].Depth != affected[j].Depth {
			return affected[i].Depth < affected[j].Depth
		}
		return affected[i].Name < affected[j].Name
	})

	ids := make([]string, len(affected))
	names := make([]string, len(affected))
	for i, a := range affected {
		ids[i] = a.ID
		names[i] = a.Name
	}

	severity := "low"
	switch {
	case len(affected) > 5:
		severity = "critical"
	case len(affected) > 2:
		severity = "high"
	case len(affected) > 0:
		severity = "medium"
	}

	return BlastRadiusResult{
		NodeID:        node.ID,
		NodeName:      node.Name,
		AffectedNodes: affected,
		AffectedIDs:   ids,
		AffectedNames: names,
		TotalAffected: len(affected),
		Severity:      severity,
	}
}
