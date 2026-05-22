package main

import (
	"encoding/json"
	"time"
)

func applyDiff(nodes map[string]*GraphNode, edges map[string]*GraphEdge, diff GraphDiff) {
	switch diff.Op {
	case DiffUpsertNode:
		if diff.Node != nil {
			n := *diff.Node
			nodes[n.ID] = &n
		}
	case DiffDeleteNode:
		delete(nodes, diff.NodeID)
	case DiffUpsertEdge:
		if diff.Edge != nil {
			e := *diff.Edge
			edges[e.ID] = &e
		}
	case DiffDeleteEdge:
		delete(edges, diff.EdgeID)
	}
}

func (s *Server) getGraphAt(projectID, env string, at time.Time) (InfraGraph, error) {
	if s.store == nil {
		return s.getGraph(projectID, env), nil
	}

	cp, err := s.store.getCheckpointBefore(projectID, env, at)
	if err != nil {
		return InfraGraph{}, err
	}

	nodes := make(map[string]*GraphNode)
	edges := make(map[string]*GraphEdge)
	var baseSeq int64

	if cp != nil {
		var base InfraGraph
		if err := json.Unmarshal(cp.GraphJSON, &base); err != nil {
			return InfraGraph{}, err
		}
		for i := range base.Nodes {
			n := base.Nodes[i]
			nodes[n.ID] = &n
		}
		for i := range base.Edges {
			e := base.Edges[i]
			edges[e.ID] = &e
		}
		baseSeq = cp.Seq
	}

	diffs, err := s.store.getDiffsAfter(projectID, env, baseSeq, at)
	if err != nil {
		return InfraGraph{}, err
	}
	for _, d := range diffs {
		applyDiff(nodes, edges, d.Diff)
	}

	return buildGraph(projectID, env, nodes, edges, at), nil
}
