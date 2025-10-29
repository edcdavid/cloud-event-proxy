// Copyright 2020 The Cloud Native Events Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/redhat-cne/cloud-event-proxy/pkg/common"
	storageClient "github.com/redhat-cne/cloud-event-proxy/pkg/storage/kubernetes"
	"github.com/redhat-cne/sdk-go/pkg/channel"
	"github.com/redhat-cne/sdk-go/pkg/types"
	v1pubsub "github.com/redhat-cne/sdk-go/v1/pubsub"
	subscriberApi "github.com/redhat-cne/sdk-go/v1/subscriber"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

const (
	testTimeout     = 10 * time.Second
	channelTimeout  = 2 * time.Second
	testAPIPort     = 9085
	testAPIPath     = "/api/ocloudNotifications/v2/"
	testNodeName    = "test-node"
	testNamespace   = "test-namespace"
	testStorePath   = "/tmp/cloud-event-proxy-test"
	metricsTestAddr = "127.0.0.1:9091"
)

// TestConfig holds test configuration and resources
type TestConfig struct {
	SCConfig          *common.SCConfiguration
	TempDir           string
	MockSubscriber    *httptest.Server
	MockPublisher     *httptest.Server
	K8sClient         *fake.Clientset
	ReceivedEvents    []ReceivedEvent
	ReceivedEventsMux sync.Mutex
	WaitGroup         *sync.WaitGroup
}

// ReceivedEvent captures events received by mock endpoints
type ReceivedEvent struct {
	Body      []byte
	Headers   http.Header
	Timestamp time.Time
}

// SetupTestEnvironment creates a test environment with all necessary mocks
func SetupTestEnvironment(t *testing.T) *TestConfig {
	// Create temporary directory for test storage
	tempDir, err := os.MkdirTemp("", "cloud-event-proxy-test-*")
	require.NoError(t, err, "Failed to create temp directory")

	tc := &TestConfig{
		TempDir:        tempDir,
		ReceivedEvents: make([]ReceivedEvent, 0),
		WaitGroup:      &sync.WaitGroup{},
	}

	// Setup mock subscriber endpoint
	tc.MockSubscriber = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		tc.ReceivedEventsMux.Lock()
		tc.ReceivedEvents = append(tc.ReceivedEvents, ReceivedEvent{
			Body:      body,
			Headers:   r.Header.Clone(),
			Timestamp: time.Now(),
		})
		tc.ReceivedEventsMux.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))

	// Setup mock publisher acknowledgment endpoint
	tc.MockPublisher = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Setup fake Kubernetes client
	tc.K8sClient = fake.NewSimpleClientset()

	// Create test ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNodeName,
			Namespace: testNamespace,
		},
		Data: make(map[string]string),
	}
	_, err = tc.K8sClient.CoreV1().ConfigMaps(testNamespace).Create(
		context.Background(), cm, metav1.CreateOptions{})
	require.NoError(t, err, "Failed to create test ConfigMap")

	// Setup SCConfiguration
	tc.SCConfig = &common.SCConfiguration{
		EventInCh:     make(chan *channel.DataChan, 100),
		EventOutCh:    make(chan *channel.DataChan, 100),
		StatusCh:      make(chan *channel.StatusChan, 50),
		CloseCh:       make(chan struct{}),
		APIPort:       testAPIPort,
		APIPath:       testAPIPath,
		StorePath:     tempDir,
		PubSubAPI:     v1pubsub.GetAPIInstance(tempDir),
		SubscriberAPI: subscriberApi.GetAPIInstance(tempDir),
		BaseURL:       nil,
		StorageType:   storageClient.EmptyDir,
		TransportHost: &common.TransportHost{
			Type:   common.HTTP,
			URL:    "http://localhost:9043",
			Host:   "localhost",
			Port:   9043,
			Scheme: "http",
			URI:    types.ParseURI("http://localhost:9043"),
		},
	}

	// Create storage client wrapper with fake clientset
	client := &storageClient.Client{}
	client.SetClientSet(tc.K8sClient)
	tc.SCConfig.K8sClient = client

	return tc
}

// TeardownTestEnvironment cleans up test resources
func TeardownTestEnvironment(t *testing.T, tc *TestConfig) {
	if tc == nil {
		return
	}

	// Close channels
	if tc.SCConfig != nil && tc.SCConfig.CloseCh != nil {
		select {
		case <-tc.SCConfig.CloseCh:
			// Already closed
		default:
			close(tc.SCConfig.CloseCh)
		}
	}

	// Stop mock servers
	if tc.MockSubscriber != nil {
		tc.MockSubscriber.Close()
	}
	if tc.MockPublisher != nil {
		tc.MockPublisher.Close()
	}

	// Clean up temp directory
	if tc.TempDir != "" {
		err := os.RemoveAll(tc.TempDir)
		if err != nil {
			t.Logf("Warning: failed to remove temp directory: %v", err)
		}
	}

	// Wait for goroutines with timeout
	done := make(chan struct{})
	go func() {
		tc.WaitGroup.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-time.After(5 * time.Second):
		t.Log("Warning: some goroutines did not finish within timeout")
	}
}

// LoadFixture loads a JSON fixture file
func LoadFixture(t *testing.T, filename string) []byte {
	fixturesDir := filepath.Join("..", "fixtures")
	data, err := os.ReadFile(filepath.Join(fixturesDir, filename))
	require.NoError(t, err, "Failed to load fixture: %s", filename)
	return data
}

// LoadFixtureAsString loads a JSON fixture file as string
func LoadFixtureAsString(t *testing.T, filename string) string {
	return string(LoadFixture(t, filename))
}

// MakeAPIRequest makes an HTTP request to the test API
func MakeAPIRequest(t *testing.T, method, path string, body []byte, baseURL string) (int, []byte, http.Header) {
	url := fmt.Sprintf("%s%s", baseURL, path)
	req, err := http.NewRequest(method, url, nil)
	require.NoError(t, err, "Failed to create request")

	if body != nil {
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err, "Failed to make request")
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "Failed to read response body")

	return resp.StatusCode, respBody, resp.Header
}

// WaitForCondition waits for a condition to be true with timeout
func WaitForCondition(t *testing.T, condition func() bool, timeout time.Duration, message string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for condition: %s", message)
}

// AssertJSONEqual compares two JSON strings for equality
func AssertJSONEqual(t *testing.T, expected, actual string) {
	var expectedJSON, actualJSON interface{}
	err := json.Unmarshal([]byte(expected), &expectedJSON)
	require.NoError(t, err, "Failed to unmarshal expected JSON")
	err = json.Unmarshal([]byte(actual), &actualJSON)
	require.NoError(t, err, "Failed to unmarshal actual JSON")
	assert.Equal(t, expectedJSON, actualJSON)
}

// StartTestAPIServer starts the REST API server for testing
func StartTestAPIServer(t *testing.T, scConfig *common.SCConfiguration) {
	err := common.StartPubSubService(scConfig)
	require.NoError(t, err, "Failed to start pub/sub service")

	// Wait for server to be ready
	WaitForCondition(t, func() bool {
		resp, err := http.Get(fmt.Sprintf("http://localhost:%d%shealth", scConfig.APIPort, scConfig.APIPath))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, "API server to be ready")
}

// GetReceivedEvents returns a copy of received events (thread-safe)
func (tc *TestConfig) GetReceivedEvents() []ReceivedEvent {
	tc.ReceivedEventsMux.Lock()
	defer tc.ReceivedEventsMux.Unlock()

	events := make([]ReceivedEvent, len(tc.ReceivedEvents))
	copy(events, tc.ReceivedEvents)
	return events
}

// ClearReceivedEvents clears the received events list (thread-safe)
func (tc *TestConfig) ClearReceivedEvents() {
	tc.ReceivedEventsMux.Lock()
	defer tc.ReceivedEventsMux.Unlock()
	tc.ReceivedEvents = make([]ReceivedEvent, 0)
}

// WaitForEvents waits for a specific number of events to be received
func (tc *TestConfig) WaitForEvents(t *testing.T, count int, timeout time.Duration) []ReceivedEvent {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		events := tc.GetReceivedEvents()
		if len(events) >= count {
			return events
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Timeout waiting for %d events, got %d", count, len(tc.GetReceivedEvents()))
	return nil
}
