package channel

import (
	"container/heap"
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/db"
)

// DeliveryStatus tracks the state of a queued message.
type DeliveryStatus int

const (
	DeliveryPending DeliveryStatus = iota
	DeliveryInFlight
	DeliveryDelivered
	DeliveryFailed
	DeliveryDeadLetter
)

// DeliveryItem represents a message in the delivery queue.
type DeliveryItem struct {
	ID             string         `json:"id"`
	Channel        string         `json:"channel"`
	RecipientID    string         `json:"recipient_id"`
	Content        string         `json:"content"`
	IdempotencyKey string         `json:"idempotency_key"`
	Status         DeliveryStatus `json:"status"`
	Attempts       int            `json:"attempts"`
	MaxAttempts    int            `json:"max_attempts"`
	NextRetryAt    time.Time      `json:"next_retry_at"`
	CreatedAt      time.Time      `json:"created_at"`
	LastError      string         `json:"last_error,omitempty"`
	index          int            // heap index
}

// backoffDelay returns the retry delay for the given attempt number.
func backoffDelay(attempt int) time.Duration {
	switch attempt {
	case 0:
		return 0
	case 1:
		return 1 * time.Second
	case 2:
		return 5 * time.Second
	case 3:
		return 30 * time.Second
	case 4:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// permanentErrorPatterns are errors that should never be retried.
var permanentErrorPatterns = []string{
	"403 forbidden", "401 unauthorized", "400 bad request",
	"blocked by the user", "bot was blocked", "chat not found",
	"user is deactivated", "bot was kicked", "peer_id_invalid",
}

// isPermanentError checks if an error message indicates a non-retryable failure.
func isPermanentError(errMsg string) bool {
	lower := strings.ToLower(errMsg)
	for _, pattern := range permanentErrorPatterns {
		if strings.Contains(lower, pattern) {
			return true
		}
	}
	return false
}

// --- Heap implementation for O(log n) next-ready scan ---

type deliveryHeap []*DeliveryItem

func (h deliveryHeap) Len() int           { return len(h) }
func (h deliveryHeap) Less(i, j int) bool { return h[i].NextRetryAt.Before(h[j].NextRetryAt) }
func (h deliveryHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *deliveryHeap) Push(x any) {
	item := x.(*DeliveryItem)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *deliveryHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

// DeliveryQueue is a persistent outbound message queue with retry and dead-letter support.
// Uses a heap for O(log n) next-ready scan (fixes roboticus O(n) VecDeque scan).
// Backed by SQLite for crash recovery (fixes roboticus in-memory-only queue).
type DeliveryQueue struct {
	mu          sync.Mutex
	pending     deliveryHeap
	deadLetters []*DeliveryItem
	store       *db.Store
}

// NewDeliveryQueue creates a queue, optionally backed by SQLite.
func NewDeliveryQueue(store *db.Store) *DeliveryQueue {
	dq := &DeliveryQueue{store: store}
	heap.Init(&dq.pending)
	if store != nil {
		dq.recoverFromStore()
	}
	return dq
}

// Enqueue adds a message to the delivery queue and returns its ID.
func (dq *DeliveryQueue) Enqueue(channel, recipientID, content string) string {
	return dq.EnqueueWithOptions(channel, recipientID, content, "", 5)
}

// EnqueueWithOptions adds a message with configurable idempotency key and max attempts.
// If an item with the same idempotency key already exists, the enqueue is skipped (dedup).
func (dq *DeliveryQueue) EnqueueWithOptions(channel, recipientID, content, idempotencyKey string, maxAttempts int) string {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	// Idempotency dedup: skip if key already exists in pending queue.
	if idempotencyKey != "" {
		for _, existing := range dq.pending {
			if existing.IdempotencyKey == idempotencyKey {
				log.Debug().Str("key", idempotencyKey).Msg("delivery enqueue skipped: duplicate idempotency key")
				return existing.ID
			}
		}
	}

	item := &DeliveryItem{
		ID:             db.NewID(),
		Channel:        channel,
		RecipientID:    recipientID,
		Content:        content,
		IdempotencyKey: idempotencyKey,
		Status:         DeliveryPending,
		MaxAttempts:    maxAttempts,
		NextRetryAt:    time.Now(),
		CreatedAt:      time.Now(),
	}

	heap.Push(&dq.pending, item)
	dq.persistItem(item)
	return item.ID
}

// DrainReady returns all items whose NextRetryAt has passed.
func (dq *DeliveryQueue) DrainReady() []*DeliveryItem {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	now := time.Now()
	var ready []*DeliveryItem

	for dq.pending.Len() > 0 {
		peek := dq.pending[0]
		if peek.NextRetryAt.After(now) {
			break
		}
		item := heap.Pop(&dq.pending).(*DeliveryItem)
		item.Status = DeliveryInFlight
		ready = append(ready, item)
	}
	return ready
}

// MarkDelivered removes an item from the queue.
func (dq *DeliveryQueue) MarkDelivered(item *DeliveryItem) {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	item.Status = DeliveryDelivered
	dq.updateItemStatus(item)
}

// RequeueFailed puts a failed item back with backoff, or dead-letters it.
func (dq *DeliveryQueue) RequeueFailed(item *DeliveryItem, errMsg string) {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	item.Attempts++
	item.LastError = errMsg

	if isPermanentError(errMsg) || item.Attempts >= item.MaxAttempts {
		item.Status = DeliveryDeadLetter
		dq.deadLetters = append(dq.deadLetters, item)
		dq.updateItemStatus(item)
		log.Warn().Str("id", item.ID).Str("channel", item.Channel).Str("error", errMsg).Str("recipient", item.RecipientID).Int("attempts", item.Attempts).Msg("message dead-lettered")
		return
	}

	item.Status = DeliveryPending
	item.NextRetryAt = time.Now().Add(backoffDelay(item.Attempts))
	heap.Push(&dq.pending, item)
	dq.updateItemStatus(item)
}

// DeadLetters returns all dead-lettered items.
func (dq *DeliveryQueue) DeadLetters() []*DeliveryItem {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	result := make([]*DeliveryItem, len(dq.deadLetters))
	copy(result, dq.deadLetters)
	return result
}

// ReplayDeadLetter moves a dead-lettered item back to pending.
// Checks in-memory first, then falls back to store (Rust parity: dual path).
func (dq *DeliveryQueue) ReplayDeadLetter(id string) bool {
	dq.mu.Lock()
	defer dq.mu.Unlock()

	// Try in-memory dead letters first.
	for i, item := range dq.deadLetters {
		if item.ID == id {
			item.Status = DeliveryPending
			item.Attempts = 0
			item.NextRetryAt = time.Now()
			item.LastError = ""
			heap.Push(&dq.pending, item)
			dq.deadLetters = append(dq.deadLetters[:i], dq.deadLetters[i+1:]...)
			dq.updateItemStatus(item)
			return true
		}
	}

	// Fallback: try store-backed replay (item may have been evicted from memory).
	if dq.store != nil {
		return dq.replayDeadLetterFromStore(id)
	}
	return false
}

// replayDeadLetterFromStore replays a dead-lettered item from the persistent store.
func (dq *DeliveryQueue) replayDeadLetterFromStore(id string) bool {
	row := dq.store.QueryRowContext(context.Background(),
		`SELECT id, channel, recipient_id, content, max_attempts FROM delivery_queue
		 WHERE id = ? AND status = ?`, id, DeliveryDeadLetter)

	var item DeliveryItem
	if err := row.Scan(&item.ID, &item.Channel, &item.RecipientID, &item.Content, &item.MaxAttempts); err != nil {
		return false
	}

	item.Status = DeliveryPending
	item.Attempts = 0
	item.NextRetryAt = time.Now()
	item.CreatedAt = time.Now()
	heap.Push(&dq.pending, &item)
	dq.updateItemStatus(&item)
	log.Info().Str("id", id).Msg("dead letter replayed from store")
	return true
}

// DeadLettersFromStore returns dead-lettered items from the persistent store.
// This catches items that were dead-lettered in a previous process lifetime.
func (dq *DeliveryQueue) DeadLettersFromStore() []*DeliveryItem {
	if dq.store == nil {
		return dq.DeadLetters()
	}
	rows, err := dq.store.QueryContext(context.Background(),
		`SELECT id, channel, recipient_id, content, attempts, max_attempts, last_error, created_at
		 FROM delivery_queue WHERE status = ? ORDER BY created_at DESC LIMIT 100`,
		DeliveryDeadLetter)
	if err != nil {
		return dq.DeadLetters()
	}
	defer func() { _ = rows.Close() }()

	var items []*DeliveryItem
	for rows.Next() {
		var item DeliveryItem
		var created string
		var lastErr *string
		if err := rows.Scan(&item.ID, &item.Channel, &item.RecipientID, &item.Content,
			&item.Attempts, &item.MaxAttempts, &lastErr, &created); err != nil {
			continue
		}
		item.Status = DeliveryDeadLetter
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if lastErr != nil {
			item.LastError = *lastErr
		}
		items = append(items, &item)
	}
	return items
}

// PendingCount returns the number of items waiting for delivery.
func (dq *DeliveryQueue) PendingCount() int {
	dq.mu.Lock()
	defer dq.mu.Unlock()
	return dq.pending.Len()
}

// persistItem writes an item to the database.
func (dq *DeliveryQueue) persistItem(item *DeliveryItem) {
	if dq.store == nil {
		return
	}
	if _, err := dq.store.ExecContext(context.Background(),
		`INSERT OR REPLACE INTO delivery_queue (id, channel, recipient_id, content, status, attempts, max_attempts, next_retry_at, created_at, last_error)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.Channel, item.RecipientID, item.Content,
		item.Status, item.Attempts, item.MaxAttempts,
		item.NextRetryAt.UTC().Format(time.RFC3339),
		item.CreatedAt.UTC().Format(time.RFC3339),
		item.LastError,
	); err != nil {
		log.Warn().Err(err).Str("id", item.ID).Msg("delivery: persist item failed")
	}
}

// updateItemStatus updates the status in the database.
func (dq *DeliveryQueue) updateItemStatus(item *DeliveryItem) {
	if dq.store == nil {
		return
	}
	if _, err := dq.store.ExecContext(context.Background(),
		`UPDATE delivery_queue SET status = ?, attempts = ?, next_retry_at = ?, last_error = ? WHERE id = ?`,
		item.Status, item.Attempts,
		item.NextRetryAt.UTC().Format(time.RFC3339),
		item.LastError, item.ID,
	); err != nil {
		log.Warn().Err(err).Str("id", item.ID).Msg("delivery: update status failed")
	}
}

// recoverFromStore reloads pending/in-flight items on startup.
func (dq *DeliveryQueue) recoverFromStore() {
	rows, err := dq.store.QueryContext(context.Background(),
		`SELECT id, channel, recipient_id, content, status, attempts, max_attempts, next_retry_at, created_at, last_error
		 FROM delivery_queue WHERE status IN (?, ?) LIMIT 2000`,
		DeliveryPending, DeliveryInFlight,
	)
	if err != nil {
		log.Warn().Err(err).Msg("delivery queue recovery failed")
		return
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		var item DeliveryItem
		var nextRetry, created string
		var lastError *string
		if err := rows.Scan(&item.ID, &item.Channel, &item.RecipientID, &item.Content,
			&item.Status, &item.Attempts, &item.MaxAttempts,
			&nextRetry, &created, &lastError); err != nil {
			continue
		}
		item.NextRetryAt, _ = time.Parse(time.RFC3339, nextRetry)
		item.CreatedAt, _ = time.Parse(time.RFC3339, created)
		if lastError != nil {
			item.LastError = *lastError
		}
		item.Status = DeliveryPending
		heap.Push(&dq.pending, &item)
		count++
	}
	if count > 0 {
		log.Info().Int("count", count).Msg("recovered delivery queue items")
	}
}

// --- Delivery Worker ---

// DeliveryWorker polls the queue and dispatches messages through channel adapters.
type DeliveryWorker struct {
	queue    *DeliveryQueue
	adapters map[string]Adapter
	interval time.Duration
}

// NewDeliveryWorker creates a worker.
func NewDeliveryWorker(queue *DeliveryQueue, adapters map[string]Adapter, interval time.Duration) *DeliveryWorker {
	return &DeliveryWorker{
		queue:    queue,
		adapters: adapters,
		interval: interval,
	}
}

// Run starts the delivery worker. Blocks until context is cancelled.
func (dw *DeliveryWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(dw.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dw.drain(ctx)
		}
	}
}

func (dw *DeliveryWorker) drain(ctx context.Context) {
	items := dw.queue.DrainReady()
	for _, item := range items {
		adapter, ok := dw.adapters[item.Channel]
		if !ok {
			dw.queue.RequeueFailed(item, fmt.Sprintf("unknown channel: %s", item.Channel))
			continue
		}

		msg := OutboundMessage{
			Content:     item.Content,
			RecipientID: item.RecipientID,
			Platform:    item.Channel,
		}

		if err := adapter.Send(ctx, msg); err != nil {
			dw.queue.RequeueFailed(item, err.Error())
		} else {
			dw.queue.MarkDelivered(item)
		}
	}
}
