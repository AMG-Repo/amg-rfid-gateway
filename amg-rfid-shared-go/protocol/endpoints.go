package protocol

// API endpoints for gateway↔cloud communication.
const (
	// EndpointSync is the endpoint for syncing readings from gateway to cloud
	EndpointSync = "/api/v1/rfid/sync"

	// EndpointPending is the endpoint for getting pending readings from cloud
	EndpointPending = "/api/v1/rfid/pending"

	// EndpointHealthGateways is the endpoint for gateway health status
	EndpointHealthGateways = "/api/v1/health/gateways"

	// EndpointMetrics is the Prometheus metrics endpoint
	EndpointMetrics = "/metrics"

	// EndpointHealth is the health check endpoint
	EndpointHealth = "/health"
)
