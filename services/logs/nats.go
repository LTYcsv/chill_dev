package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

func subscribeNATS(nc *nats.Conn, store *LogStore) {
	nc.Subscribe("logs.line.*", func(m *nats.Msg) {
		var l LogLine
		if err := json.Unmarshal(m.Data, &l); err != nil {
			return
		}
		if l.TS.IsZero() {
			l.TS = time.Now()
		}
		store.Ingest(l)
	})

	terminalSubs := map[string]func(map[string]string) string{
		"build.completed":  func(p map[string]string) string { return "build completed: " + p["image_tag"] },
		"build.failed":     func(p map[string]string) string { return "build failed: " + p["error"] },
		"runtime.deployed": func(p map[string]string) string { return "deployment successful" },
		"deploy.failed":    func(p map[string]string) string { return "deployment failed: " + p["error"] },
	}

	for subj, msgFn := range terminalSubs {
		subj, msgFn := subj, msgFn
		nc.Subscribe(subj, func(m *nats.Msg) {
			var payload map[string]string
			if err := json.Unmarshal(m.Data, &payload); err != nil {
				return
			}
			deployID := payload["deployment_id"]
			if deployID == "" {
				return
			}
			store.Ingest(LogLine{
				DeploymentID: deployID,
				Line:         msgFn(payload),
				Source:       "system",
				TS:           time.Now(),
			})
			store.MarkDone(deployID)
		})
	}

	log.Println("[logs] NATS subscriptions active: logs.line.*, build/runtime events")
}
