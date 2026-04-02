package agent

import (
	"testing"
	"time"
)

func TestRetrievalStrategy_SelectMode(t *testing.T) {
	tests := []struct {
		name               string
		embeddingAvailable bool
		corpusSize         int
		query              string
		sessionAge         time.Duration
		wantMode           RetrievalMode
	}{
		{
			name:               "no embeddings defaults to keyword",
			embeddingAvailable: false,
			corpusSize:         100,
			query:              "test",
			sessionAge:         10 * time.Minute,
			wantMode:           RetrievalKeyword,
		},
		{
			name:               "young session uses recency",
			embeddingAvailable: true,
			corpusSize:         100,
			query:              "test",
			sessionAge:         2 * time.Minute,
			wantMode:           RetrievalRecency,
		},
		{
			name:               "large corpus uses ANN",
			embeddingAvailable: true,
			corpusSize:         2000,
			query:              "test",
			sessionAge:         30 * time.Minute,
			wantMode:           RetrievalANN,
		},
		{
			name:               "default is hybrid",
			embeddingAvailable: true,
			corpusSize:         500,
			query:              "test",
			sessionAge:         10 * time.Minute,
			wantMode:           RetrievalHybrid,
		},
		{
			name:               "exact threshold boundary uses ANN",
			embeddingAvailable: true,
			corpusSize:         1000,
			query:              "test",
			sessionAge:         10 * time.Minute,
			wantMode:           RetrievalANN,
		},
		{
			name:               "just under threshold uses hybrid",
			embeddingAvailable: true,
			corpusSize:         999,
			query:              "test",
			sessionAge:         10 * time.Minute,
			wantMode:           RetrievalHybrid,
		},
		{
			name:               "exact 5min boundary uses recency",
			embeddingAvailable: true,
			corpusSize:         100,
			query:              "test",
			sessionAge:         5 * time.Minute,
			wantMode:           RetrievalRecency,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := NewRetrievalStrategy(tt.embeddingAvailable, tt.corpusSize)
			mode := rs.SelectMode(tt.query, tt.sessionAge)
			if mode != tt.wantMode {
				t.Errorf("SelectMode() = %v, want %v", mode, tt.wantMode)
			}
		})
	}
}

func TestRetrievalStrategy_CustomThreshold(t *testing.T) {
	rs := NewRetrievalStrategy(true, 500)
	rs.ANNThreshold = 400 // lower threshold

	mode := rs.SelectMode("test", 10*time.Minute)
	if mode != RetrievalANN {
		t.Errorf("with custom threshold, mode = %v, want ANN", mode)
	}
}
