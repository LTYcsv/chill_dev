package main

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
)

type Server struct {
	mu    sync.RWMutex
	nodes map[string]*GraphNode
	edges map[string]*GraphEdge
	store *TTStore
	seqMu sync.Mutex
	seqs  map[string]int64
}

func newServer(nodes map[string]*GraphNode, edges map[string]*GraphEdge, store *TTStore) *Server {
	s := &Server{
		nodes: nodes,
		edges: edges,
		store: store,
		seqs:  make(map[string]int64),
	}
	if store != nil {
		for k, v := range store.maxSeqPerCombo() {
			s.seqs[k] = v
		}
	}
	return s
}

func (s *Server) nextSeq(projectID, env string) int64 {
	s.seqMu.Lock()
	defer s.seqMu.Unlock()
	key := projectID + ":" + env
	s.seqs[key]++
	return s.seqs[key]
}

func (s *Server) record(projectID, env, source string, diff GraphDiff) {
	if s.store == nil {
		return
	}
	seq := s.nextSeq(projectID, env)
	s.store.saveDiff(projectID, env, seq, source, diff)
	if seq%100 == 0 {
		g := s.getGraph(projectID, env)
		s.store.saveCheckpoint(projectID, env, seq, g)
		log.Printf("[graph] checkpoint at seq=%d for %s/%s", seq, projectID, env)
	}
}

func (s *Server) saveInitialCheckpoints() {
	if s.store == nil {
		return
	}
	combos := make(map[string][2]string)
	s.mu.RLock()
	for _, n := range s.nodes {
		key := n.ProjectID + ":" + n.Environment
		combos[key] = [2]string{n.ProjectID, n.Environment}
	}
	s.mu.RUnlock()

	for _, combo := range combos {
		projectID, env := combo[0], combo[1]
		cp, _ := s.store.getCheckpointBefore(projectID, env, time.Now())
		if cp == nil {
			g := s.getGraph(projectID, env)
			s.store.saveCheckpoint(projectID, env, 0, g)
			log.Printf("[graph] initial checkpoint saved for %s/%s", projectID, env)
		}
	}
}

func (s *Server) startDailyCheckpoint() {
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		for range ticker.C {
			if s.store == nil {
				continue
			}
			combos := make(map[string][2]string)
			s.mu.RLock()
			for _, n := range s.nodes {
				key := n.ProjectID + ":" + n.Environment
				combos[key] = [2]string{n.ProjectID, n.Environment}
			}
			s.mu.RUnlock()
			s.seqMu.Lock()
			for _, combo := range combos {
				projectID, env := combo[0], combo[1]
				seq := s.seqs[projectID+":"+env]
				g := s.getGraph(projectID, env)
				s.store.saveCheckpoint(projectID, env, seq, g)
				log.Printf("[graph] daily checkpoint for %s/%s", projectID, env)
			}
			s.seqMu.Unlock()
		}
	}()
}

func (s *Server) UpsertNode(n GraphNode, source string) {
	s.mu.Lock()
	s.nodes[n.ID] = &n
	s.mu.Unlock()
	s.record(n.ProjectID, n.Environment, source, GraphDiff{Op: DiffUpsertNode, Node: &n})
}

func (s *Server) DeleteNode(id, projectID, env, source string) {
	s.mu.Lock()
	delete(s.nodes, id)
	s.mu.Unlock()
	s.record(projectID, env, source, GraphDiff{Op: DiffDeleteNode, NodeID: id})
}

func (s *Server) UpsertEdge(e GraphEdge, projectID, env, source string) {
	s.mu.Lock()
	s.edges[e.ID] = &e
	s.mu.Unlock()
	s.record(projectID, env, source, GraphDiff{Op: DiffUpsertEdge, Edge: &e})
}

func (s *Server) DeleteEdge(id, projectID, env, source string) {
	s.mu.Lock()
	delete(s.edges, id)
	s.mu.Unlock()
	s.record(projectID, env, source, GraphDiff{Op: DiffDeleteEdge, EdgeID: id})
}

func (s *Server) handleNATSEvent(serviceID string, status NodeStatus, source string) {
	s.mu.Lock()
	var snap GraphNode
	var found bool
	for _, n := range s.nodes {
		if n.ID == serviceID || n.Name == serviceID || (n.Metadata != nil && n.Metadata["service_id"] == serviceID) {
			n.Status = status
			snap = *n
			found = true
			break
		}
	}
	s.mu.Unlock()
	if found {
		s.record(snap.ProjectID, snap.Environment, source, GraphDiff{Op: DiffUpsertNode, Node: &snap})
	}
}

func (s *Server) subscribeToNATS(nc *nats.Conn) {
	nc.Subscribe("runtime.deployed", func(m *nats.Msg) {
		var p map[string]string
		if json.Unmarshal(m.Data, &p) == nil {
			s.handleNATSEvent(p["service_id"], NodeRunning, "runtime.deployed")
		}
	})
	nc.Subscribe("deploy.failed", func(m *nats.Msg) {
		var p map[string]string
		if json.Unmarshal(m.Data, &p) == nil {
			s.handleNATSEvent(p["service_id"], NodeDegraded, "deploy.failed")
		}
	})
}
