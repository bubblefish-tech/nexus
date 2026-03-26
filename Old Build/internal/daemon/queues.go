package daemon

import (
"context"
"log"
"sync"

"github.com/shawnsammartano-hub/bubblefish/internal/core"
"github.com/shawnsammartano-hub/bubblefish/internal/destinations"
)

type QueueManager struct {
queues map[string]chan core.TranslatedPayload
mu     sync.RWMutex
}

func NewQueueManager() *QueueManager {
return &QueueManager{
queues: make(map[string]chan core.TranslatedPayload),
}
}

func (qm *QueueManager) StartWorkers(destinationsList []destinations.DestinationPlugin) {
qm.mu.Lock()
defer qm.mu.Unlock()

for _, dest := range destinationsList {
name := dest.Name()

ch := make(chan core.TranslatedPayload, 100)
qm.queues[name] = ch

go func(d destinations.DestinationPlugin, queue chan core.TranslatedPayload) {
ctx := context.Background()

for payload := range queue {
if err := d.Write(ctx, payload); err != nil {
log.Printf("write error to destination %s: %v", d.Name(), err)
}
}
}(dest, ch)
}
}

func (qm *QueueManager) Enqueue(p core.TranslatedPayload) {
qm.mu.RLock()
defer qm.mu.RUnlock()

queue, ok := qm.queues[p.Dest]
if !ok {
log.Printf("no queue found for destination: %s", p.Dest)
return
}

queue <- p
}
