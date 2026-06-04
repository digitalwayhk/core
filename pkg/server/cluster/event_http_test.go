package cluster_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/digitalwayhk/core/pkg/server/cluster"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capturedRequest holds a single HTTP request body decoded into a map.
type capturedRequest struct {
	path string
	body map[string]interface{}
}

// noticeCaptureSrv creates an httptest.Server that records every incoming
// POST body. The returned channel receives one item per POST request.
// The server is automatically closed when the test finishes.
func noticeCaptureSrv(t *testing.T) (*httptest.Server, <-chan capturedRequest) {
	t.Helper()
	ch := make(chan capturedRequest, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]interface{}
		_ = json.Unmarshal(raw, &body)
		select {
		case ch <- capturedRequest{path: r.URL.Path, body: body}:
		default:
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

// parseServerAddr extracts host and integer port from an httptest.Server URL.
func parseServerAddr(t *testing.T, rawURL string) (host string, port int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	require.NoError(t, err)
	host = u.Hostname()
	p, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return host, p
}

// newBrokerProvider creates a fresh LocalProvider for CrossNodeBroker tests and
// registers localNodeID (DataCenterID=1, MachineID=1) so it is visible to the
// broker but filtered out when broadcasting to "peers". The returned provider is
// started; the test's Cleanup stops it.
func newBrokerProvider(t *testing.T, serviceName, localNodeID string) *cluster.LocalProvider {
	t.Helper()
	p := cluster.NewLocalProvider(10*time.Second, 10*time.Second, 30*time.Second)
	p.Start()
	t.Cleanup(func() { p.Close() })

	err := p.Register(context.Background(), &cluster.NodeInfo{
		ID:           localNodeID,
		ServiceName:  serviceName,
		DataCenterID: 1,
		MachineID:    1,
		Address:      "127.0.0.1",
		Port:         9999,
		Weight:       1,
	})
	require.NoError(t, err)
	return p
}

// registerPeerNode adds a peer node (DataCenterID=1, MachineID=2) whose HTTP
// address matches the given httptest.Server.
func registerPeerNode(t *testing.T, p *cluster.LocalProvider, serviceName, peerID string, srv *httptest.Server) {
	t.Helper()
	host, port := parseServerAddr(t, srv.URL)
	err := p.Register(context.Background(), &cluster.NodeInfo{
		ID:           peerID,
		ServiceName:  serviceName,
		DataCenterID: 1,
		MachineID:    2,
		Address:      host,
		Port:         port,
		Weight:       1,
	})
	require.NoError(t, err)
}

// TestCrossNodeBroker_OnSubscriptionChange_PostsToPeer verifies that when the
// local node calls OnSubscriptionChange, the broker sends an HTTP POST to every
// running peer node's /api/servermanage/ws/subscription endpoint.
func TestCrossNodeBroker_OnSubscriptionChange_PostsToPeer(t *testing.T) {
	const svcName = "crosstest-subchange"
	const localID = "node-a"
	const peerID = "node-b"

	srv, received := noticeCaptureSrv(t)
	p := newBrokerProvider(t, svcName, localID)
	registerPeerNode(t, p, svcName, peerID, srv)

	broker := cluster.NewCrossNodeNoticeBroker(p, svcName, localID)

	broker.OnSubscriptionChange("/api/test/route", 42, true)

	// Await the async POST.
	var got capturedRequest
	require.Eventually(t, func() bool {
		select {
		case got = <-received:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond, "peer should receive subscription change POST")

	assert.Equal(t, "/api/servermanage/ws/subscription", got.path)
	assert.Equal(t, "/api/test/route", got.body["route_path"])
	assert.Equal(t, float64(42), got.body["hash"])
	assert.Equal(t, true, got.body["active"])
	assert.Equal(t, localID, got.body["node_id"])
}

// TestCrossNodeBroker_ForwardNotice_PostsToSubscribedPeerOnly verifies that
// ForwardNotice only POSTs to peer nodes that have registered subscriptions for
// the given routePath+hash. A peer without a matching subscription must NOT
// receive a POST.
func TestCrossNodeBroker_ForwardNotice_PostsToSubscribedPeerOnly(t *testing.T) {
	const svcName = "crosstest-fwdnotice"
	const localID = "node-a"
	const subscribedPeerID = "node-b"

	srv, received := noticeCaptureSrv(t)
	p := newBrokerProvider(t, svcName, localID)
	registerPeerNode(t, p, svcName, subscribedPeerID, srv)

	broker := cluster.NewCrossNodeNoticeBroker(p, svcName, localID)

	// Only node-b subscribes to /route/orders hash=99.
	broker.UpdatePeerSubscription("/route/orders", 99, subscribedPeerID, true)

	// ForwardNotice should send exactly one POST to node-b.
	broker.ForwardNotice(context.Background(), "/route/orders", 99, map[string]string{"event": "orderCreated"})

	var got capturedRequest
	require.Eventually(t, func() bool {
		select {
		case got = <-received:
			return true
		default:
			return false
		}
	}, 2*time.Second, 10*time.Millisecond, "subscribed peer should receive notice POST")

	assert.Equal(t, "/api/servermanage/ws/notice", got.path)
	assert.Equal(t, "/route/orders", got.body["route_path"])
	assert.Equal(t, float64(99), got.body["hash"])
	require.NotNil(t, got.body["message"])
}

// TestCrossNodeBroker_ForwardNotice_NoSubscribers_NoPosts verifies that when no
// peer has subscribed to the given routePath+hash, ForwardNotice does not send
// any HTTP requests.
func TestCrossNodeBroker_ForwardNotice_NoSubscribers_NoPosts(t *testing.T) {
	const svcName = "crosstest-nosubscribers"
	const localID = "node-a"

	var callCount int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&callCount, 1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	p := newBrokerProvider(t, svcName, localID)
	host, port := parseServerAddr(t, srv.URL)
	_ = p.Register(context.Background(), &cluster.NodeInfo{
		ID: "node-b", ServiceName: svcName, Address: host, Port: port,
		DataCenterID: 1, MachineID: 2, Weight: 1,
	})

	broker := cluster.NewCrossNodeNoticeBroker(p, svcName, localID)
	// No UpdatePeerSubscription call — no peer has subscribed.
	broker.ForwardNotice(context.Background(), "/route/orders", 99, "msg")

	// Give any hypothetical goroutine enough time to fire.
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int64(0), atomic.LoadInt64(&callCount), "no POST should be sent when no peer has subscribed")
}

// TestCrossNodeBroker_DrainAndStop_BroadcastsRemovalAndStops verifies that
// DrainAndStop broadcasts active=false for every locally registered subscription
// and that subsequent ForwardNotice calls are silently dropped.
func TestCrossNodeBroker_DrainAndStop_BroadcastsRemovalAndStops(t *testing.T) {
	const svcName = "crosstest-drain"
	const localID = "node-a"
	const peerID = "node-b"

	// Phase 1: Register local subscriptions WITHOUT the peer node present, so
	// the initial OnSubscriptionChange calls don't fire any HTTP requests.
	p := newBrokerProvider(t, svcName, localID)
	broker := cluster.NewCrossNodeNoticeBroker(p, svcName, localID)

	broker.OnSubscriptionChange("/route/x", 1, true)
	broker.OnSubscriptionChange("/route/y", 2, true)

	// Phase 2: Now add the peer node and create the capture server.
	srv, received := noticeCaptureSrv(t)
	registerPeerNode(t, p, svcName, peerID, srv)

	// Phase 3: DrainAndStop should broadcast active=false for both subscriptions.
	broker.DrainAndStop(context.Background())

	// Collect all removal POSTs (expect 2).
	var activeFlags []bool
	deadline := time.After(2 * time.Second)
loop:
	for len(activeFlags) < 2 {
		select {
		case req := <-received:
			assert.Equal(t, "/api/servermanage/ws/subscription", req.path)
			active, _ := req.body["active"].(bool)
			activeFlags = append(activeFlags, active)
		case <-deadline:
			break loop
		}
	}

	require.Len(t, activeFlags, 2, "DrainAndStop should broadcast removal for each local subscription")
	for _, a := range activeFlags {
		assert.False(t, a, "all removal broadcasts should have active=false")
	}

	// Phase 4: After stop, ForwardNotice must be a no-op (broker is stopped).
	broker.UpdatePeerSubscription("/route/x", 1, peerID, true)
	broker.ForwardNotice(context.Background(), "/route/x", 1, "ignored")
	time.Sleep(100 * time.Millisecond)

	select {
	case <-received:
		t.Error("ForwardNotice should be a no-op after DrainAndStop")
	default:
		// expected: nothing sent
	}
}
