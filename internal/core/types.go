package core

import "time"

type TranslatedPayload struct {
ID        string            json:"id"
Source    string            json:"source"
Dest      string            json:"dest"
Content   string            json:"content"
Metadata  map[string]string json:"metadata"
Timestamp time.Time         json:"timestamp"
}

type QueryRequest struct {
Source string json:"source"
Dest   string json:"dest"
Query  string json:"query"
Limit  int    json:"limit"
}
