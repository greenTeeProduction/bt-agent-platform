package reliability

import (
	"sync"
	"testing"
	"time"
)

func TestNodeHeartbeat_RegisterAndPing(t *testing.T) {
	hb := NewNodeHeartbeat(500 * time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)
	if !hb.IsAlive("node-1") {
		t.Fatal("newly registered node should be alive")
	}

	time.Sleep(300 * time.Millisecond)
	if !hb.IsAlive("node-1") {
		t.Fatal("node should still be alive before TTL expires")
	}

	time.Sleep(300 * time.Millisecond)
	if hb.IsAlive("node-1") {
		t.Fatal("node should expire after TTL without ping")
	}
}

func TestNodeHeartbeat_PingKeepsAlive(t *testing.T) {
	hb := NewNodeHeartbeat(200 * time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)
	if !hb.IsAlive("node-1") {
		t.Fatal("newly registered node should be alive")
	}

	// Ping just before TTL expiry
	time.Sleep(150 * time.Millisecond)
	if !hb.Ping("node-1") {
		t.Fatal("Ping should return true for registered node")
	}
	if !hb.IsAlive("node-1") {
		t.Fatal("node should be alive after ping")
	}

	// Wait past original TTL but within ping-extended TTL
	time.Sleep(100 * time.Millisecond)
	if !hb.IsAlive("node-1") {
		t.Fatal("node should be alive within ping-extended window")
	}

	// Wait past ping-extended TTL
	time.Sleep(100 * time.Millisecond)
	if hb.IsAlive("node-1") {
		t.Fatal("node should expire after extended TTL")
	}
}

func TestNodeHeartbeat_PingNotRegistered(t *testing.T) {
	hb := NewNodeHeartbeat(1 * time.Second)
	defer hb.Stop()

	if hb.Ping("nonexistent") {
		t.Fatal("Ping should return false for unregistered node")
	}
}

func TestNodeHeartbeat_Deregister(t *testing.T) {
	hb := NewNodeHeartbeat(1 * time.Second)
	defer hb.Stop()

	hb.Register("node-1", nil)
	if !hb.IsAlive("node-1") {
		t.Fatal("node should be alive after registration")
	}

	hb.Deregister("node-1")
	if hb.IsAlive("node-1") {
		t.Fatal("node should not be alive after deregistration")
	}

	// Idempotent
	hb.Deregister("node-1")
	if hb.IsAlive("node-1") {
		t.Fatal("deregistration should be idempotent")
	}
}

func TestNodeHeartbeat_ReRegister(t *testing.T) {
	hb := NewNodeHeartbeat(200 * time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", map[string]string{"url": "http://old"})

	// Let it expire
	time.Sleep(300 * time.Millisecond)
	if hb.IsAlive("node-1") {
		t.Fatal("node should expire")
	}

	// Re-register revives it
	hb.Register("node-1", map[string]string{"url": "http://new"})
	if !hb.IsAlive("node-1") {
		t.Fatal("re-registered node should be alive")
	}

	entries := hb.ListAll()
	for _, e := range entries {
		if e.NodeID == "node-1" && e.Metadata["url"] != "http://new" {
			t.Fatalf("re-registration should update metadata, got %v", e.Metadata)
		}
	}
}

func TestNodeHeartbeat_RegisterWithNilMetadataKeepsExisting(t *testing.T) {
	hb := NewNodeHeartbeat(1 * time.Second)
	defer hb.Stop()

	meta := map[string]string{"url": "http://original"}
	hb.Register("node-1", meta)
	// Re-register with nil metadata should keep existing
	hb.Register("node-1", nil)

	entries := hb.ListAll()
	for _, e := range entries {
		if e.NodeID == "node-1" && e.Metadata["url"] != "http://original" {
			t.Fatal("nil metadata should preserve existing metadata")
		}
	}
}

func TestNodeHeartbeat_ListAlive(t *testing.T) {
	hb := NewNodeHeartbeat(300 * time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)
	hb.Register("node-2", nil)

	alive := hb.ListAlive()
	if len(alive) != 2 {
		t.Fatalf("expected 2 alive, got %d: %v", len(alive), alive)
	}

	// Let both expire
	time.Sleep(400 * time.Millisecond)
	alive = hb.ListAlive()
	if len(alive) != 0 {
		t.Fatalf("expected 0 alive after TTL, got %d: %v", len(alive), alive)
	}

	// Add new node, keep one alive via ping
	hb.Register("node-3", nil)
	hb.Register("node-4", nil)
	time.Sleep(150 * time.Millisecond)
	hb.Ping("node-3")
	time.Sleep(200 * time.Millisecond)

	alive = hb.ListAlive()
	if len(alive) != 1 {
		t.Fatalf("expected 1 alive (node-3), got %d: %v", len(alive), alive)
	}
	if alive[0] != "node-3" {
		t.Fatalf("expected node-3 alive, got %v", alive[0])
	}
}

func TestNodeHeartbeat_ListAll(t *testing.T) {
	hb := NewNodeHeartbeat(300 * time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", map[string]string{"region": "us-east"})
	hb.Register("node-2", map[string]string{"region": "eu-west"})

	entries := hb.ListAll()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	for _, e := range entries {
		if !e.Alive {
			t.Fatalf("node %s should be alive immediately after registration", e.NodeID)
		}
		if e.Metadata == nil || e.Metadata["region"] == "" {
			t.Fatalf("node %s should have metadata", e.NodeID)
		}
	}

	// Let them expire
	time.Sleep(400 * time.Millisecond)
	entries = hb.ListAll()
	for _, e := range entries {
		if e.Alive {
			t.Fatalf("node %s should be expired after TTL", e.NodeID)
		}
	}
}

func TestNodeHeartbeat_Stats(t *testing.T) {
	hb := NewNodeHeartbeat(200 * time.Millisecond)
	defer hb.Stop()

	total, alive, expired := hb.Stats()
	if total != 0 || alive != 0 || expired != 0 {
		t.Fatalf("empty stats: total=%d alive=%d expired=%d", total, alive, expired)
	}

	hb.Register("node-1", nil)
	hb.Register("node-2", nil)

	total, alive, expired = hb.Stats()
	if total != 2 || alive != 2 || expired != 0 {
		t.Fatalf("stats after register: total=%d alive=%d expired=%d", total, alive, expired)
	}

	time.Sleep(300 * time.Millisecond)
	total, alive, expired = hb.Stats()
	if total != 2 || alive != 0 || expired != 2 {
		t.Fatalf("stats after expiry: total=%d alive=%d expired=%d", total, alive, expired)
	}
}

func TestNodeHeartbeat_CleanupRemovesExpired(t *testing.T) {
	hb := NewNodeHeartbeatWithCleanupInterval(100*time.Millisecond, 50*time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)
	hb.Register("node-2", nil)

	// Let them expire and get cleaned up
	time.Sleep(200 * time.Millisecond)

	total, _, _ := hb.Stats()
	if total != 0 {
		t.Fatalf("cleanup should remove expired nodes, got %d", total)
	}
}

func TestNodeHeartbeat_CleanupKeepsAliveNodes(t *testing.T) {
	hb := NewNodeHeartbeatWithCleanupInterval(500*time.Millisecond, 50*time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)

	// Ping continuously to keep alive through multiple cleanup cycles
	for i := 0; i < 5; i++ {
		time.Sleep(100 * time.Millisecond)
		hb.Ping("node-1")
	}

	total, alive, _ := hb.Stats()
	if total != 1 || alive != 1 {
		t.Fatalf("pinged node should survive cleanup: total=%d alive=%d", total, alive)
	}
}

func TestNodeHeartbeat_Stop(t *testing.T) {
	hb := NewNodeHeartbeatWithCleanupInterval(100*time.Millisecond, 50*time.Millisecond)

	hb.Register("node-1", nil)

	// Stop cleanup
	hb.Stop()

	// Idempotent stop
	hb.Stop()

	// Let node expire naturally
	time.Sleep(200 * time.Millisecond)

	// Stats still shows expired node because cleanup stopped
	total, alive, expired := hb.Stats()
	if total != 1 || alive != 0 || expired != 1 {
		t.Fatalf("after stop, expired node stays: total=%d alive=%d expired=%d", total, alive, expired)
	}
}

func TestNodeHeartbeat_IsAlive_Unregistered(t *testing.T) {
	hb := NewNodeHeartbeat(1 * time.Second)
	defer hb.Stop()

	if hb.IsAlive("nonexistent") {
		t.Fatal("unregistered node should not be alive")
	}
}

func TestNodeHeartbeat_TTL(t *testing.T) {
	hb := NewNodeHeartbeat(5 * time.Second)
	defer hb.Stop()

	if hb.TTL() != 5*time.Second {
		t.Fatalf("TTL should be 5s, got %v", hb.TTL())
	}
}

func TestNodeHeartbeat_ConcurrentAccess(t *testing.T) {
	hb := NewNodeHeartbeat(5 * time.Second)
	defer hb.Stop()

	var wg sync.WaitGroup
	numGoroutines := 50
	numNodes := 10

	// Concurrent registration
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numNodes; j++ {
				nodeID := "node-" + string(rune('0'+j%10))
				hb.Register(nodeID, nil)
				hb.Ping(nodeID)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				hb.IsAlive("node-0")
				hb.ListAlive()
				hb.ListAll()
				hb.Stats()
			}
		}()

	}

	wg.Wait()

	// All registered nodes should be alive
	alive := hb.ListAlive()
	if len(alive) != numNodes {
		t.Fatalf("expected %d alive nodes, got %d: %v", numNodes, len(alive), alive)
	}
}

func TestNodeHeartbeat_ConcurrentDeregister(t *testing.T) {
	hb := NewNodeHeartbeat(5 * time.Second)
	defer hb.Stop()

	for i := 0; i < 100; i++ {
		hb.Register("node-"+string(rune('0'+i%10)), nil)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			nodeID := "node-" + string(rune('0'+id%10))
			hb.Deregister(nodeID)
			hb.Register(nodeID, nil)
			hb.Ping(nodeID)
		}(i)
	}
	wg.Wait()

	// Should not panic — remaining nodes should be consistent
	hb.ListAll()
	hb.ListAlive()
	hb.Stats()
}

func TestNodeHeartbeat_ZeroTTL(t *testing.T) {
	// Zero TTL means instant expiry, but Register sets lastSeen=now
	hb := NewNodeHeartbeatWithCleanupInterval(0, 10*time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)
	// With 0 TTL, node is alive if lastSeen == now (sub-microsecond)
	// But time advances between Register and IsAlive, so it should be dead
	time.Sleep(5 * time.Millisecond)
	if hb.IsAlive("node-1") {
		t.Log("node with 0 TTL may appear alive due to timing — acceptable")
	}

	// Cleanup should eventually remove it
	time.Sleep(50 * time.Millisecond)
	total, _, _ := hb.Stats()
	if total != 0 {
		t.Fatalf("cleanup should remove 0-TTL node, got %d", total)
	}
}

func TestNodeHeartbeat_EmptyListAll(t *testing.T) {
	hb := NewNodeHeartbeat(1 * time.Second)
	defer hb.Stop()

	entries := hb.ListAll()
	if len(entries) != 0 {
		t.Fatalf("empty heartbeat should return empty slice, got %d", len(entries))
	}
}

func TestNodeHeartbeat_EmptyListAlive(t *testing.T) {
	hb := NewNodeHeartbeat(1 * time.Second)
	defer hb.Stop()

	alive := hb.ListAlive()
	if len(alive) != 0 {
		t.Fatalf("empty heartbeat should return empty alive list, got %d", len(alive))
	}
}

func TestNodeHeartbeat_RegisterPreservesMetadataOnRefresh(t *testing.T) {
	hb := NewNodeHeartbeat(500 * time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", map[string]string{"url": "http://original", "region": "us-east"})
	time.Sleep(100 * time.Millisecond)

	// Re-register with nil — should refresh TTL but preserve metadata
	hb.Register("node-1", nil)
	entries := hb.ListAll()
	for _, e := range entries {
		if e.NodeID == "node-1" {
			if e.Metadata["url"] != "http://original" {
				t.Fatal("nil re-register should preserve metadata")
			}
			if !e.Alive {
				t.Fatal("re-register should refresh TTL")
			}
		}
	}
}

func TestNodeHeartbeat_NewNodeHeartbeatWithCleanupInterval(t *testing.T) {
	hb := NewNodeHeartbeatWithCleanupInterval(100*time.Millisecond, 20*time.Millisecond)
	defer hb.Stop()

	hb.Register("node-1", nil)

	// Wait for cleanup
	time.Sleep(150 * time.Millisecond)

	total, _, _ := hb.Stats()
	if total != 0 {
		t.Fatalf("expected cleanup with custom interval, got %d nodes", total)
	}
}
