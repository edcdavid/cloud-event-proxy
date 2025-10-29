# Cloud Event Proxy - External Interface Testing Plan

## Overview
This document outlines a comprehensive testing strategy for all external interfaces of the cloud-event-proxy project. The goal is to create tests that validate external behavior, allowing internal refactoring while maintaining contract compatibility.

## Project Architecture Summary

The cloud-event-proxy is a sidecar container that:
1. Provides low-latency event delivery from K8s infrastructure to CNFs
2. Exposes REST APIs for publisher/subscriber management
3. Supports plugin architecture (PTP operator, mock plugins)
4. Integrates with Kubernetes ConfigMaps for persistent storage
5. Publishes Prometheus metrics
6. Implements CloudEvents v2 specification

---

## 1. External Interaction Points

### 1.1 REST API Endpoints (HTTP Server)
**Base Path:** `/api/ocloudNotifications/v2/`

| Endpoint | Method | Purpose | Input | Output |
|----------|--------|---------|-------|--------|
| `/publishers` | POST | Create publisher | PubSub JSON | Publisher with ID |
| `/publishers` | GET | List publishers | - | Array of publishers |
| `/publishers/{publisherId}` | GET | Get publisher | Publisher ID | Publisher details |
| `/publishers/{publisherId}` | DELETE | Delete publisher | Publisher ID | Status code |
| `/subscriptions` | POST | Create subscription | PubSub JSON | Subscription with ID |
| `/subscriptions` | GET | List subscriptions | - | Array of subscriptions |
| `/subscriptions/{subscriptionId}` | GET | Get subscription | Subscription ID | Subscription details |
| `/subscriptions/{subscriptionId}` | DELETE | Delete subscription | Subscription ID | Status code |
| `/subscriptions/status/{subscriptionId}` | PUT | Ping subscription | Subscription ID | Status |
| `/create/event` | POST | Publish event | CloudEvent JSON | Status code |
| `/health` | GET | Health check | - | Health status |
| `/log` | POST | Log event | Log data | Status code |

### 1.2 Metrics Endpoint
**Endpoint:** `/metrics` (port 9091 by default)

**Metrics Exposed:**
- `cne_amqp_events_published` - Number of events published
- `cne_amqp_events_received` - Number of events received
- `cne_transport_connections_reset` - Connection resets
- `cne_transport_sender` - Number of senders created
- `cne_transport_receiver` - Number of receivers created
- `cne_api_events_published` - Events published via API
- `cne_api_subscriptions` - Active subscriptions count
- `cne_api_publishers` - Active publishers count
- PTP-specific metrics (when plugin enabled)

### 1.3 REST Client Outbound Calls

The proxy makes HTTP POST calls to external endpoints:

| Call Type | Destination | Payload | Expected Response |
|-----------|-------------|---------|-------------------|
| Event delivery | Subscriber endpoint URI | CloudEvent v2 JSON | 204 No Content |
| Publisher acknowledgment | Publisher endpoint URI | `{eventId, status}` | Any 2xx |

**Error Handling:**
- DNS errors trigger subscriber cleanup after 10 failures
- Connection refused triggers subscriber cleanup
- Timeout errors trigger subscriber cleanup

### 1.4 Kubernetes Client API

**Operations:**
- `ConfigMap.Create(namespace, nodeName)` - Create subscription storage
- `ConfigMap.Get(namespace, nodeName)` - Retrieve subscriptions
- `ConfigMap.Update(namespace, nodeName, data)` - Update subscriptions
- In-cluster authentication via service account

### 1.5 Plugin Interface Contract

**Expected Function Signature:**
```go
func Start(wg *sync.WaitGroup, scConfig *common.SCConfiguration, fn func(e interface{}) error) error
```

**Plugin Responsibilities:**
- Initialize using provided SCConfiguration
- Register publishers via `common.CreatePublisher()`
- Publish events to `scConfig.EventOutCh`
- Handle cleanup on `scConfig.CloseCh` signal
- Respect WaitGroup for graceful shutdown

**Available Plugins:**
- `ptp_operator_plugin.so` - PTP event generation
- `mock_plugin.so` - Testing/mock events

### 1.6 Event Channel Communication

**Channels:**
- `EventInCh chan *channel.DataChan` (buffer: 100) - Inbound events
- `EventOutCh chan *channel.DataChan` (buffer: 100) - Outbound events
- `StatusCh chan *channel.StatusChan` (buffer: 50) - Status updates
- `CloseCh chan struct{}` - Shutdown signal

**DataChan Structure:**
```go
type DataChan struct {
    Address            string        // Resource address
    Data               *event.Event  // CloudEvent data
    Status             Status        // NEW, SUCCESS, FAILED
    Type               Type          // EVENT, STATUS, SUBSCRIBER, PUBLISHER
    ProcessEventFn     func          // Optional event processor
    OnReceiveOverrideFn func         // Optional receive handler
    ClientID           uuid.UUID     // Client identifier
}
```

### 1.7 CloudEvents Protocol Compliance

**Format:** CloudEvents v2.0 specification

**Required Fields:**
- `id` - Event identifier (UUID)
- `type` - Event type (e.g., `event.sync.ptp-status.ptp-state-change`)
- `source` - Event source URI
- `specversion` - "1.0"
- `datacontenttype` - "application/json"
- `time` - ISO 8601 timestamp

**Data Payload:**
```json
{
  "version": "v1.0",
  "values": [{
    "resource": "/cluster/node/ptp",
    "dataType": "notification|metric",
    "valueType": "enumeration|decimal64.3",
    "value": "ACQUIRING-SYNC"
  }]
}
```

### 1.8 Environment Variables

| Variable | Purpose | Default |
|----------|---------|---------|
| `NODE_NAME` | Node identifier | (required) |
| `NODE_IP` | Node IP address | - |
| `NAME_SPACE` | K8s namespace | - |
| `PTP_PLUGIN` | Enable PTP plugin | false |
| `MOCK_PLUGIN` | Enable mock plugin | false |
| `LOG_LEVEL` | Logging level | debug |

### 1.9 Command-Line Flags

| Flag | Purpose | Default |
|------|---------|---------|
| `--metrics-addr` | Metrics bind address | :9091 |
| `--store-path` | Publisher/subscription storage | . |
| `--transport-host` | Transport service URL | (required) |
| `--api-port` | API server port | 9043 |
| `--api-version` | API version | 2.0 |

---

## 2. Testing Strategy

### 2.1 Test Categories

#### A. Contract Tests
Validate external API contracts remain stable across internal changes.

#### B. Integration Tests  
Test interaction with real external dependencies (K8s, HTTP clients).

#### C. Protocol Compliance Tests
Ensure CloudEvents v2 and OpenAPI spec compliance.

#### D. End-to-End Tests
Full workflow tests from event creation to delivery.

#### E. Error Handling Tests
Validate graceful degradation and retry logic.

### 2.2 Test Tools & Frameworks

- **Go testing framework** - Unit and integration tests
- **testify** - Assertions and mocking (already in vendor/)
- **ginkgo/gomega** - BDD-style integration tests (already in vendor/)
- **httptest** - HTTP server/client testing
- **Kubernetes fake clientset** - K8s API mocking
- **Prometheus testutil** - Metrics validation

---

## 3. Detailed Test Plan

### 3.1 REST API Tests

**File:** `test/integration/rest_api_test.go`

**Tests:**
1. **Publisher CRUD Operations**
   - Create publisher with valid payload → 201 Created
   - Create publisher with invalid payload → 400 Bad Request
   - Get publisher by ID → 200 OK
   - Get non-existent publisher → 404 Not Found
   - List all publishers → 200 OK
   - Delete publisher → 204 No Content
   - Delete non-existent publisher → 404 Not Found

2. **Subscription CRUD Operations**
   - Create subscription with valid payload → 201 Created
   - Create subscription with invalid payload → 400 Bad Request
   - Get subscription by ID → 200 OK
   - Get non-existent subscription → 404 Not Found
   - List all subscriptions → 200 OK
   - Delete subscription → 204 No Content
   - Ping subscription status → 200 OK

3. **Event Publishing**
   - Publish event with valid publisher → 202 Accepted
   - Publish event without publisher → 404 Not Found
   - Publish malformed event → 400 Bad Request

4. **Health Endpoint**
   - GET /health → 200 OK

5. **Content Negotiation**
   - All endpoints accept/return `application/json`
   - Proper Content-Type headers

6. **Concurrent Operations**
   - Multiple simultaneous publisher creations
   - Race-free subscription management

### 3.2 Metrics Tests

**File:** `test/integration/metrics_test.go`

**Tests:**
1. **Metrics Endpoint Availability**
   - GET /metrics returns 200 OK
   - Returns Prometheus text format

2. **Counter Metrics**
   - Event published increments counter
   - Event received increments counter
   - Subscription created increments gauge
   - Publisher created increments gauge

3. **Metric Accuracy**
   - Counters never decrease
   - Gauges reflect actual state
   - Labels applied correctly (address, status)

4. **PTP Plugin Metrics** (when enabled)
   - PTP-specific metrics exposed
   - Metrics update on event generation

### 3.3 REST Client Tests

**File:** `test/integration/restclient_test.go`

**Tests:**
1. **Event Delivery**
   - POST to subscriber endpoint
   - Correct CloudEvent format
   - Timeout handling (2s default)
   - Retry logic validation

2. **Publisher Acknowledgments**
   - POST acknowledgment on event success
   - POST acknowledgment on event failure
   - Handle unreachable publisher endpoint

3. **Error Scenarios**
   - DNS resolution failure
   - Connection refused
   - Timeout exceeded
   - HTTP 4xx/5xx responses

4. **Subscriber Cleanup**
   - 10 consecutive failures trigger deletion
   - ConfigMap updated on deletion
   - Metrics updated on deletion

### 3.4 Kubernetes Client Tests

**File:** `test/integration/kubernetes_client_test.go`

**Tests:**
1. **ConfigMap Operations**
   - Create ConfigMap for subscriptions
   - Get existing ConfigMap
   - Update ConfigMap with subscriber data
   - Delete subscriber from ConfigMap
   - Handle ConfigMap already exists

2. **Storage Initialization**
   - Load subscriptions from ConfigMap on startup
   - Persist to file system (store-path)
   - Fallback to EmptyDir on K8s errors

3. **Error Handling**
   - Retry logic on ConfigMap creation failure
   - Graceful degradation without K8s access
   - Handle namespace/node name missing

4. **Mock K8s Client**
   - Use fake clientset for testing
   - Validate API call patterns

### 3.5 Plugin Interface Tests

**File:** `test/integration/plugin_interface_test.go`

**Tests:**
1. **Plugin Loading**
   - Load PTP plugin successfully
   - Load mock plugin successfully
   - Handle plugin not found error
   - Handle invalid plugin (wrong signature)

2. **Plugin Contract**
   - Start function called with correct parameters
   - WaitGroup handled properly
   - SCConfiguration passed correctly
   - Error propagation

3. **Plugin Lifecycle**
   - Plugin initializes before event processing
   - Plugin respects CloseCh shutdown
   - Graceful cleanup on termination

4. **Mock Plugin for Testing**
   - Generate test events
   - Validate publisher creation
   - Validate event flow to channels

### 3.6 Channel Communication Tests

**File:** `test/integration/channel_test.go`

**Tests:**
1. **EventInCh Processing**
   - Send EVENT to channel → processed
   - Send STATUS to channel → processed
   - Send SUBSCRIBER to channel → processed
   - Send PUBLISHER to channel → processed

2. **EventOutCh Processing**
   - Receive EVENT from channel → deliver to subscriber
   - Receive STATUS from channel → update metrics
   - Receive SUBSCRIBER from channel → update ConfigMap
   - Success/Failure status handling

3. **Channel Flow**
   - Event flows from InCh to OutCh
   - ProcessEventFn called when provided
   - OnReceiveOverrideFn called when provided

4. **Buffering Behavior**
   - Channels don't block under load
   - Handle buffer full scenarios
   - Graceful degradation

5. **Shutdown Signal**
   - CloseCh stops all processing
   - No goroutine leaks
   - Proper cleanup

### 3.7 CloudEvents Protocol Tests

**File:** `test/integration/cloudevents_test.go`

**Tests:**
1. **Event Structure Validation**
   - Required fields present (id, type, source, specversion)
   - Correct specversion ("1.0")
   - Valid datacontenttype
   - ISO 8601 timestamp format

2. **Event Serialization**
   - Marshal to JSON successfully
   - Unmarshal from JSON successfully
   - Binary mode (if supported)
   - Structured mode

3. **Event Types**
   - PTP state change events
   - OS clock sync events
   - Clock class change events
   - Custom event types

4. **Data Payload**
   - Values array structure
   - Resource address format
   - DataType enumeration
   - ValueType validation

5. **CloudEvents v2 Compliance**
   - Passes CloudEvents validation
   - Compatible with other CloudEvents consumers
   - Context attributes validation

### 3.8 End-to-End Tests

**File:** `test/e2e/e2e_test.go`

**Tests:**
1. **Publisher to Subscriber Flow**
   - Create publisher via API
   - Create subscription via API
   - Publish event via API
   - Verify event delivered to subscriber
   - Verify metrics updated

2. **Plugin Event Generation**
   - Load PTP plugin
   - Plugin creates publishers
   - Plugin generates events
   - Events delivered to subscribers
   - Acknowledgment sent to plugin

3. **Multi-Subscriber Scenario**
   - Multiple subscriptions to same resource
   - Event delivered to all subscribers
   - Individual failure handling
   - Metrics per subscriber

4. **Subscriber Failure Recovery**
   - Subscriber temporarily down
   - Retry mechanism activates
   - Subscriber comes back up
   - Events resume delivery

5. **ConfigMap Persistence**
   - Create subscription
   - Restart proxy pod
   - Subscription loaded from ConfigMap
   - Events continue to deliver

6. **Full Lifecycle**
   - Start proxy
   - Load plugin
   - Create publishers/subscriptions
   - Generate events
   - Deliver events
   - Clean shutdown
   - No resource leaks

### 3.9 Error Handling Tests

**File:** `test/integration/error_handling_test.go`

**Tests:**
1. **Invalid Input Handling**
   - Malformed JSON → 400
   - Missing required fields → 400
   - Invalid UUIDs → 400
   - Invalid resource addresses → 400

2. **Resource Not Found**
   - Non-existent publisher → 404
   - Non-existent subscription → 404
   - Event for non-existent publisher → 404

3. **External Dependency Failures**
   - K8s API unavailable → fallback to EmptyDir
   - Subscriber endpoint unreachable → retry then delete
   - Publisher acknowledgment fails → log and continue

4. **Concurrent Request Handling**
   - Race conditions prevented
   - Thread-safe data structures
   - No deadlocks

5. **Resource Exhaustion**
   - Channel buffer full → graceful handling
   - High event rate → no drops
   - Memory limits respected

### 3.10 Backward Compatibility Tests

**File:** `test/integration/compatibility_test.go`

**Tests:**
1. **API Version Compatibility**
   - v1 API returns error (deprecated)
   - v2 API fully functional

2. **Storage Format**
   - Load legacy subscription files
   - Migrate to new format if needed
   - ConfigMap backward compatible

3. **Event Format Evolution**
   - Handle events with new fields
   - Handle events missing optional fields
   - Version field validation

---

## 4. Test Implementation Phases

### Phase 1: Foundation (Week 1)
- [ ] Set up test framework structure
- [ ] Create test utilities and helpers
- [ ] Implement REST API contract tests
- [ ] Implement metrics endpoint tests

### Phase 2: Core Integration (Week 2)
- [ ] Implement REST client tests
- [ ] Implement Kubernetes client tests
- [ ] Implement channel communication tests
- [ ] Create mock implementations

### Phase 3: Plugin & Protocol (Week 3)
- [ ] Implement plugin interface tests
- [ ] Implement CloudEvents protocol tests
- [ ] Create test plugin for validation
- [ ] Implement error handling tests

### Phase 4: E2E & Validation (Week 4)
- [ ] Implement end-to-end test scenarios
- [ ] Implement backward compatibility tests
- [ ] Performance and load testing
- [ ] Documentation and CI integration

---

## 5. Test Infrastructure Requirements

### 5.1 Mock Services

**HTTP Mock Server:**
```go
// Mock subscriber endpoint
func mockSubscriberServer() *httptest.Server {
    return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Validate CloudEvent format
        // Return 204 No Content
    }))
}
```

**Kubernetes Fake Client:**
```go
import "k8s.io/client-go/kubernetes/fake"

func setupFakeK8sClient() kubernetes.Interface {
    return fake.NewSimpleClientset()
}
```

### 5.2 Test Fixtures

**Directory:** `test/fixtures/`
- `publisher_valid.json` - Valid publisher payload
- `publisher_invalid.json` - Invalid publisher payload
- `subscription_valid.json` - Valid subscription payload
- `event_valid.json` - Valid CloudEvent
- `configmap_sample.yaml` - Sample ConfigMap
- `ptp_events/` - PTP-specific test events

### 5.3 CI/CD Integration

**Makefile targets:**
```makefile
test-unit: ## Run unit tests
	go test -v ./pkg/... ./plugins/...

test-integration: ## Run integration tests
	go test -v -tags=integration ./test/integration/...

test-e2e: ## Run e2e tests
	go test -v -tags=e2e ./test/e2e/...

test-all: test-unit test-integration test-e2e ## Run all tests

test-coverage: ## Generate coverage report
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out
```

**GitHub Actions workflow:**
- Run tests on PR
- Run tests on main branch
- Require tests pass before merge
- Coverage threshold enforcement (>80%)

---

## 6. Success Criteria

### 6.1 Test Coverage Targets

| Component | Target Coverage |
|-----------|----------------|
| REST API handlers | 95%+ |
| REST client | 90%+ |
| Kubernetes client | 90%+ |
| Plugin loader | 90%+ |
| Channel processing | 90%+ |
| Common utilities | 85%+ |
| **Overall** | **85%+** |

### 6.2 Test Quality Metrics

- ✅ All external interfaces have contract tests
- ✅ All error paths covered
- ✅ No flaky tests (must pass 100 consecutive runs)
- ✅ Tests run in <5 minutes total
- ✅ No external dependencies required (use mocks)
- ✅ Tests can run in parallel
- ✅ Clear test documentation

### 6.3 Refactoring Confidence

After tests are complete, you should be able to:
- ✅ Change internal data structures without breaking tests
- ✅ Refactor channel processing logic
- ✅ Optimize performance without behavioral changes
- ✅ Replace internal libraries
- ✅ Reorganize package structure

---

## 7. Test Maintenance Strategy

### 7.1 Test Documentation

- Each test file has package-level documentation
- Complex tests have inline comments
- Test names clearly describe scenario
- Use `testify/suite` for related tests

### 7.2 Test Data Management

- Fixtures in version control
- Generated test data cleaned up after tests
- Mock data representative of production
- No hardcoded production data

### 7.3 Continuous Improvement

- Review test failures immediately
- Update tests when requirements change
- Refactor tests to reduce duplication
- Monitor test execution time
- Regular test coverage analysis

---

## 8. Appendix

### 8.1 Related Documentation

- [Development Guide](docs/development.md)
- [Metrics Documentation](docs/metrics.md)
- [Plugin README](plugins/README.md)
- [PTP Configurations](plugins/ptp_operator/docs/configurations.md)

### 8.2 Example Test Structure

```go
package integration_test

import (
    "testing"
    "github.com/stretchr/testify/suite"
)

type RestAPITestSuite struct {
    suite.Suite
    server *httptest.Server
    config *common.SCConfiguration
}

func (s *RestAPITestSuite) SetupTest() {
    // Initialize test environment
}

func (s *RestAPITestSuite) TearDownTest() {
    // Cleanup
}

func (s *RestAPITestSuite) TestCreatePublisher() {
    // Test implementation
}

func TestRestAPITestSuite(t *testing.T) {
    suite.Run(t, new(RestAPITestSuite))
}
```

### 8.3 Key Design Principles

1. **Test Isolation** - Each test runs independently
2. **Deterministic** - Tests produce same result every time
3. **Fast** - Tests complete quickly
4. **Comprehensive** - Cover happy path and edge cases
5. **Maintainable** - Easy to understand and modify
6. **Automated** - Run without manual intervention
7. **External Focus** - Test behavior, not implementation

---

## Conclusion

This comprehensive test plan ensures that all external interfaces of the cloud-event-proxy are thoroughly tested. By focusing on external behavior rather than internal implementation, these tests will enable confident refactoring while maintaining compatibility with existing consumers and producers of the proxy service.

The phased implementation approach allows for incremental progress and early validation of the testing strategy. Once complete, the test suite will serve as both a safety net for refactoring and living documentation of the system's external contracts.

