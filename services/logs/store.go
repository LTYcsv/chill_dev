package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type LogLine struct {
	DeploymentID string    `json:"deployment_id"`
	Line         string    `json:"line"`
	Source       string    `json:"source"`
	TS           time.Time `json:"ts"`
}

const maxLinesPerDeployment = 2000
const redisKeyPrefix = "logs:dep:"
const redisDoneTTL = 24 * time.Hour

type DeploymentLog struct {
	mu    sync.Mutex
	lines []LogLine
	done  bool
	subs  map[chan LogLine]struct{}
}

func newDeploymentLog() *DeploymentLog {
	return &DeploymentLog{subs: make(map[chan LogLine]struct{})}
}

func (d *DeploymentLog) append(l LogLine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.lines) >= maxLinesPerDeployment {
		d.lines = d.lines[1:]
	}
	d.lines = append(d.lines, l)
	if !d.done {
		for ch := range d.subs {
			select {
			case ch <- l:
			default:
			}
		}
	}
}

func (d *DeploymentLog) markDone() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.done = true
	for ch := range d.subs {
		close(ch)
	}
	d.subs = make(map[chan LogLine]struct{})
}

func (d *DeploymentLog) subscribe() (chan LogLine, []LogLine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	snapshot := make([]LogLine, len(d.lines))
	copy(snapshot, d.lines)
	if d.done {
		return nil, snapshot
	}
	ch := make(chan LogLine, 128)
	d.subs[ch] = struct{}{}
	return ch, snapshot
}

func (d *DeploymentLog) unsubscribe(ch chan LogLine) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.subs, ch)
}

type LogStore struct {
	mu   sync.RWMutex
	data map[string]*DeploymentLog
	rdb  *redis.Client
}

func NewLogStore(rdb *redis.Client) *LogStore {
	return &LogStore{data: make(map[string]*DeploymentLog), rdb: rdb}
}

func redisKey(deploymentID string) string     { return redisKeyPrefix + deploymentID }
func redisDoneKey(deploymentID string) string { return redisKeyPrefix + deploymentID + ":done" }

func (s *LogStore) loadFromRedis(deploymentID string) *DeploymentLog {
	dl := newDeploymentLog()
	if s.rdb == nil {
		return dl
	}
	ctx := context.Background()
	raw, err := s.rdb.LRange(ctx, redisKey(deploymentID), 0, -1).Result()
	if err != nil || len(raw) == 0 {
		return dl
	}
	for _, r := range raw {
		var l LogLine
		if json.Unmarshal([]byte(r), &l) == nil {
			dl.lines = append(dl.lines, l)
		}
	}
	if exists, _ := s.rdb.Exists(ctx, redisDoneKey(deploymentID)).Result(); exists > 0 {
		dl.done = true
	}
	return dl
}

func (s *LogStore) getOrCreate(deploymentID string) *DeploymentLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	if dl, ok := s.data[deploymentID]; ok {
		return dl
	}
	dl := s.loadFromRedis(deploymentID)
	s.data[deploymentID] = dl
	return dl
}

func (s *LogStore) get(deploymentID string) *DeploymentLog {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[deploymentID]
}

func (s *LogStore) count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func (s *LogStore) Ingest(l LogLine) {
	s.getOrCreate(l.DeploymentID).append(l)
	if s.rdb != nil {
		data, err := json.Marshal(l)
		if err == nil {
			go s.rdb.RPush(context.Background(), redisKey(l.DeploymentID), data)
		}
	}
}

func (s *LogStore) MarkDone(deploymentID string) {
	if dl := s.get(deploymentID); dl != nil {
		dl.markDone()
	}
	if s.rdb != nil {
		go s.rdb.Set(context.Background(), redisDoneKey(deploymentID), "1", redisDoneTTL)
	}
}
