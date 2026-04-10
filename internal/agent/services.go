// Package agent: ServiceManager — manages the agent's service catalog and request lifecycle.
//
// Ported from Rust: crates/roboticus-agent/src/services.rs
//
// A service is something the agent can offer to other agents or users (e.g., code review,
// data analysis). Each request follows the lifecycle:
//
//	Quoted → PaymentPending → PaymentVerified → InProgress → Completed | Failed | Refunded

package agent

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"roboticus/internal/core"
)

// ServiceStatus represents the state of a service request.
type ServiceStatus string

const (
	ServiceStatusQuoted          ServiceStatus = "quoted"
	ServiceStatusPaymentPending  ServiceStatus = "payment_pending"
	ServiceStatusPaymentVerified ServiceStatus = "payment_verified"
	ServiceStatusInProgress      ServiceStatus = "in_progress"
	ServiceStatusCompleted       ServiceStatus = "completed"
	ServiceStatusFailed          ServiceStatus = "failed"
	ServiceStatusRefunded        ServiceStatus = "refunded"
)

// ServiceDefinition describes a service the agent can offer.
type ServiceDefinition struct {
	ID                    string   `json:"id"`
	Name                  string   `json:"name"`
	Description           string   `json:"description"`
	PriceUSDC             float64  `json:"price_usdc"`
	CapabilitiesRequired  []string `json:"capabilities_required,omitempty"`
	MaxConcurrent         int      `json:"max_concurrent,omitempty"`
	EstimatedDurationSecs int64    `json:"estimated_duration_seconds,omitempty"`
}

// ServiceRequest tracks a request for a service from a client.
type ServiceRequest struct {
	ID          string          `json:"id"`
	ServiceID   string          `json:"service_id"`
	Requester   string          `json:"requester"`
	Parameters  json.RawMessage `json:"parameters"`
	Status      ServiceStatus   `json:"status"`
	PaymentTx   string          `json:"payment_tx,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

// ServiceManager manages the service catalog and request lifecycle.
type ServiceManager struct {
	mu             sync.Mutex
	catalog        map[string]ServiceDefinition
	requests       map[string]*ServiceRequest
	requestCounter uint64
}

// NewServiceManager creates a new service manager.
func NewServiceManager() *ServiceManager {
	return &ServiceManager{
		catalog:  make(map[string]ServiceDefinition),
		requests: make(map[string]*ServiceRequest),
	}
}

// RegisterService adds a service to the catalog.
func (sm *ServiceManager) RegisterService(svc ServiceDefinition) error {
	if svc.ID == "" {
		return core.NewError(core.ErrConfig, "service id cannot be empty")
	}
	if svc.PriceUSDC < 0 {
		return core.NewError(core.ErrConfig, "price cannot be negative")
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	log.Info().Str("id", svc.ID).Str("name", svc.Name).Float64("price", svc.PriceUSDC).Msg("registered service")
	sm.catalog[svc.ID] = svc
	return nil
}

// GetService returns a service definition by ID.
func (sm *ServiceManager) GetService(serviceID string) (ServiceDefinition, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	svc, ok := sm.catalog[serviceID]
	return svc, ok
}

// ListServices returns all available services.
func (sm *ServiceManager) ListServices() []ServiceDefinition {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	svcs := make([]ServiceDefinition, 0, len(sm.catalog))
	for _, svc := range sm.catalog {
		svcs = append(svcs, svc)
	}
	return svcs
}

// CreateQuote creates a quote for a service request.
func (sm *ServiceManager) CreateQuote(serviceID, requester string, params json.RawMessage) (*ServiceRequest, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	svc, ok := sm.catalog[serviceID]
	if !ok {
		return nil, core.NewError(core.ErrConfig, fmt.Sprintf("service '%s' not found", serviceID))
	}

	sm.requestCounter++
	reqID := fmt.Sprintf("req_%d", sm.requestCounter)

	req := &ServiceRequest{
		ID:         reqID,
		ServiceID:  serviceID,
		Requester:  requester,
		Parameters: params,
		Status:     ServiceStatusQuoted,
		CreatedAt:  time.Now().UTC(),
	}

	log.Info().Str("request_id", reqID).Str("service", svc.Name).Float64("price", svc.PriceUSDC).Msg("created service quote")
	sm.requests[reqID] = req
	return req, nil
}

// RecordPayment transitions a request from Quoted to PaymentVerified.
func (sm *ServiceManager) RecordPayment(requestID, txHash string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	req, ok := sm.requests[requestID]
	if !ok {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' not found", requestID))
	}
	if req.Status != ServiceStatusQuoted {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' is not in quoted state", requestID))
	}

	req.PaymentTx = txHash
	req.Status = ServiceStatusPaymentVerified
	log.Info().Str("request_id", requestID).Str("tx", txHash).Msg("payment recorded")
	return nil
}

// StartFulfillment transitions a request from PaymentVerified to InProgress.
func (sm *ServiceManager) StartFulfillment(requestID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	req, ok := sm.requests[requestID]
	if !ok {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' not found", requestID))
	}
	if req.Status != ServiceStatusPaymentVerified {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' payment not verified", requestID))
	}

	req.Status = ServiceStatusInProgress
	log.Debug().Str("request_id", requestID).Msg("fulfillment started")
	return nil
}

// CompleteFulfillment transitions a request from InProgress to Completed.
func (sm *ServiceManager) CompleteFulfillment(requestID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	req, ok := sm.requests[requestID]
	if !ok {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' not found", requestID))
	}
	if req.Status != ServiceStatusInProgress {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' is not in progress", requestID))
	}

	req.Status = ServiceStatusCompleted
	now := time.Now().UTC()
	req.CompletedAt = &now
	log.Info().Str("request_id", requestID).Msg("fulfillment completed")
	return nil
}

// FailFulfillment marks a request as failed.
func (sm *ServiceManager) FailFulfillment(requestID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	req, ok := sm.requests[requestID]
	if !ok {
		return core.NewError(core.ErrConfig, fmt.Sprintf("request '%s' not found", requestID))
	}

	req.Status = ServiceStatusFailed
	log.Warn().Str("request_id", requestID).Msg("fulfillment failed")
	return nil
}

// GetRequest returns a request by ID.
func (sm *ServiceManager) GetRequest(requestID string) (*ServiceRequest, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	req, ok := sm.requests[requestID]
	return req, ok
}

// RequestsByStatus returns all requests with the given status.
func (sm *ServiceManager) RequestsByStatus(status ServiceStatus) []*ServiceRequest {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var result []*ServiceRequest
	for _, req := range sm.requests {
		if req.Status == status {
			result = append(result, req)
		}
	}
	return result
}

// TotalRevenue calculates total revenue from completed requests.
func (sm *ServiceManager) TotalRevenue() float64 {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	var total float64
	for _, req := range sm.requests {
		if req.Status == ServiceStatusCompleted {
			if svc, ok := sm.catalog[req.ServiceID]; ok {
				total += svc.PriceUSDC
			}
		}
	}
	return total
}

// CatalogSize returns the number of registered services.
func (sm *ServiceManager) CatalogSize() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.catalog)
}

// RequestCount returns the total number of requests.
func (sm *ServiceManager) RequestCount() int {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return len(sm.requests)
}
