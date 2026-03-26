package daemon

import (
"bufio"
"encoding/json"
"os"
"sync"

"github.com/shawnsammartano-hub/bubblefish/internal/core"
)

type WAL struct {
path string
mu   sync.Mutex
}

func NewWAL(path string) (*WAL, error) {
f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
if err != nil {
return nil, err
}
f.Close()

return &WAL{path: path}, nil
}

func (w *WAL) Append(p core.TranslatedPayload) error {
w.mu.Lock()
defer w.mu.Unlock()

f, err := os.OpenFile(w.path, os.O_APPEND|os.O_WRONLY, 0644)
if err != nil {
return err
}
defer f.Close()

encoder := json.NewEncoder(f)
if err := encoder.Encode(p); err != nil {
return err
}

return f.Sync()
}

func (w *WAL) Replay() ([]core.TranslatedPayload, error) {
w.mu.Lock()
defer w.mu.Unlock()

f, err := os.Open(w.path)
if err != nil {
return nil, err
}
defer f.Close()

var entries []core.TranslatedPayload
scanner := bufio.NewScanner(f)

for scanner.Scan() {
var p core.TranslatedPayload
if err := json.Unmarshal(scanner.Bytes(), &p); err != nil {
return nil, err
}
entries = append(entries, p)
}

if err := scanner.Err(); err != nil {
return nil, err
}

return entries, nil
}
