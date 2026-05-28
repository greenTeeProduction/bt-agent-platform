package reliability

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
)

// ─── Redis Queue Tests ───────────────────────────────────────────────────────

// testRedisAddr returns the Redis address to use for tests.
// Tests will be skipped if Redis is unreachable.
func testRedisAddr() string {
	return "localhost:6379"
}

// testRedisClient creates a fresh client and flushes state before each test.
func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr()})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
		return nil
	}
	// Clean up test keys before running
	client.Del(ctx, "bt:queue:tasks", "bt:queue:priority", "bt:test:queue", "bt:test:priority")
	return client
}

func TestRedisQueue_EnqueueDequeue(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rq := NewRedisQueueFromClient(client, "bt:test:queue")
	rq.Enqueue("task1")
	rq.Enqueue("task2")

	if rq.Len() != 2 {
		t.Errorf("expected len=2, got %d", rq.Len())
	}

	got := rq.Dequeue()
	if got != "task1" {
		t.Errorf("expected 'task1', got %q", got)
	}

	if rq.Len() != 1 {
		t.Errorf("expected len=1 after dequeue, got %d", rq.Len())
	}
}

func TestRedisQueue_Peek(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rq := NewRedisQueueFromClient(client, "bt:test:queue")
	rq.Enqueue("first")

	peeked := rq.Peek()
	if peeked != "first" {
		t.Errorf("peek expected 'first', got %q", peeked)
	}
	if rq.Len() != 1 {
		t.Error("peek should not remove")
	}
}

func TestRedisQueue_Empty(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rq := NewRedisQueueFromClient(client, "bt:test:queue")

	if rq.Len() != 0 {
		t.Errorf("empty queue should have len 0, got %d", rq.Len())
	}

	task := rq.Dequeue()
	if task != "" {
		t.Errorf("empty dequeue should return empty string, got %q", task)
	}

	peeked := rq.Peek()
	if peeked != "" {
		t.Errorf("empty peek should return empty string, got %q", peeked)
	}
}

func TestRedisQueue_DefaultKey(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	// Test default key construction
	rq := NewRedisQueueFromClient(client, "")
	if rq.key != "bt:queue:tasks" {
		t.Errorf("expected default key 'bt:queue:tasks', got %q", rq.key)
	}
}

func TestRedisQueue_NewRedisQueue_UsesAddr(t *testing.T) {
	// Only test if Redis is running on localhost
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr()})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	client.Close()

	rq := NewRedisQueue(testRedisAddr(), "bt:test:queue")
	defer rq.Close()

	rq.Enqueue("hello")
	if rq.Len() != 1 {
		t.Errorf("expected len=1, got %d", rq.Len())
	}
	task := rq.Dequeue()
	if task != "hello" {
		t.Errorf("expected 'hello', got %q", task)
	}
	// Clean up
	rq.client.Del(ctx, "bt:test:queue")
}

// ─── Redis Priority Queue Tests ──────────────────────────────────────────────

func TestRedisPriorityQueue_DequeueOrder(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "bt:test:priority")
	rpq.Enqueue("low task", "agent-a", PriorityLow)
	rpq.Enqueue("critical task", "agent-b", PriorityCritical)
	rpq.Enqueue("high task", "agent-c", PriorityHigh)
	rpq.Enqueue("medium task", "agent-d", PriorityMedium)

	expected := []Priority{PriorityCritical, PriorityHigh, PriorityMedium, PriorityLow}
	for i, exp := range expected {
		task := rpq.Dequeue()
		if task.Priority != exp {
			t.Errorf("dequeue %d: expected %s, got %s (task=%q)", i, exp, task.Priority, task.Task)
		}
		if task.ID == "" {
			t.Error("task ID should not be empty")
		}
	}
}

func TestRedisPriorityQueue_Peek(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "bt:test:priority")
	rpq.Enqueue("low", "a", PriorityLow)
	rpq.Enqueue("critical", "b", PriorityCritical)

	peeked := rpq.Peek()
	if peeked.Priority != PriorityCritical {
		t.Errorf("peek expected critical, got %s", peeked.Priority)
	}
	if rpq.Len() != 2 {
		t.Error("peek should not remove")
	}
}

func TestRedisPriorityQueue_Empty(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "bt:test:priority")

	task := rpq.Dequeue()
	if task.ID != "" {
		t.Error("empty dequeue should return zero PriorityTask")
	}
	if rpq.Len() != 0 {
		t.Error("empty queue should have len 0")
	}

	peeked := rpq.Peek()
	if peeked.ID != "" {
		t.Error("empty peek should return zero PriorityTask")
	}
}

func TestRedisPriorityQueue_Purge(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "bt:test:priority")
	rpq.Enqueue("a", "x", PriorityMedium)
	rpq.Enqueue("b", "y", PriorityHigh)

	if rpq.Len() != 2 {
		t.Fatalf("expected 2 before purge, got %d", rpq.Len())
	}

	rpq.Purge()
	if rpq.Len() != 0 {
		t.Errorf("after purge, len should be 0, got %d", rpq.Len())
	}
}

func TestRedisPriorityQueue_DefaultKey(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "")
	if rpq.key != "bt:queue:priority" {
		t.Errorf("expected default key 'bt:queue:priority', got %q", rpq.key)
	}
}

func TestRedisPriorityQueue_NewRedisPriorityQueue_UsesAddr(t *testing.T) {
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: testRedisAddr()})
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available: %v", err)
		return
	}
	client.Close()

	rpq := NewRedisPriorityQueue(testRedisAddr(), "bt:test:priority")
	defer rpq.Close()

	id := rpq.Enqueue("test task", "agent-x", PriorityCritical)
	if id == "" {
		t.Error("expected non-empty ID")
	}
	if rpq.Len() != 1 {
		t.Errorf("expected len=1, got %d", rpq.Len())
	}

	task := rpq.Dequeue()
	if task.Task != "test task" {
		t.Errorf("expected 'test task', got %q", task.Task)
	}

	// Clean up
	rpq.client.Del(ctx, "bt:test:priority")
}

func TestRedisPriorityQueue_IDIsUnique(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "bt:test:priority")

	id1 := rpq.Enqueue("task1", "a", PriorityHigh)
	id2 := rpq.Enqueue("task2", "a", PriorityHigh)

	if id1 == id2 {
		t.Errorf("expected unique IDs, got %q and %q", id1, id2)
	}
	if id1 == "" || id2 == "" {
		t.Error("IDs should not be empty")
	}
}

func TestRedisPriorityQueue_SamePriority(t *testing.T) {
	client := testRedisClient(t)
	if client == nil {
		return
	}
	defer client.Close()

	rpq := NewRedisPriorityQueueFromClient(client, "bt:test:priority")
	rpq.Enqueue("task 1", "agent", PriorityMedium)
	rpq.Enqueue("task 2", "agent", PriorityMedium)
	rpq.Enqueue("task 3", "agent", PriorityMedium)

	// All three should be dequeued at PriorityMedium
	for i := 0; i < 3; i++ {
		task := rpq.Dequeue()
		if task.Priority != PriorityMedium {
			t.Errorf("task %d: expected medium priority, got %s", i, task.Priority)
		}
	}
	if rpq.Len() != 0 {
		t.Errorf("expected empty queue, got %d", rpq.Len())
	}
}
