package reliability

import (
	"context"
	"encoding/json"
	"time"

	"github.com/redis/go-redis/v9"
)

// ─── Redis Queue (Queue interface) ───────────────────────────────────────────

// RedisQueue provides a Redis LIST-backed FIFO task queue.
// Multiple processes can share a single RedisQueue for distributed
// task processing. Thread-safe via Redis atomic operations.
//
// Uses:
//   - LPUSH for enqueue (head)
//   - RPOP for dequeue (tail)
//   - LINDEX 0 for peek
//   - LLEN for length
type RedisQueue struct {
	client *redis.Client
	key    string
}

// NewRedisQueue creates a Redis-backed task queue.
//
// The addr should be a Redis connection string like "localhost:6379".
// The key is the Redis list key used for storage.
// Pass "" for key to use the default "bt:queue:tasks".
func NewRedisQueue(addr, key string) *RedisQueue {
	if key == "" {
		key = "bt:queue:tasks"
	}
	return &RedisQueue{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		key:    key,
	}
}

// NewRedisQueueFromClient creates a Redis-backed task queue from an
// existing Redis client (useful for sharing connections or testing).
func NewRedisQueueFromClient(client *redis.Client, key string) *RedisQueue {
	if key == "" {
		key = "bt:queue:tasks"
	}
	return &RedisQueue{client: client, key: key}
}

// Enqueue adds a task to the tail of the queue (FIFO).
func (rq *RedisQueue) Enqueue(task string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rq.client.LPush(ctx, rq.key, task)
}

// Dequeue removes and returns the task at the head of the queue.
// Returns empty string if the queue is empty.
func (rq *RedisQueue) Dequeue() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	task, err := rq.client.RPop(ctx, rq.key).Result()
	if err == redis.Nil {
		return ""
	}
	// Non-nil errors are silently ignored — the queue treats them as empty.
	if err != nil {
		return ""
	}
	return task
}

// Peek returns the task at the head without removing it.
// Returns empty string if the queue is empty.
func (rq *RedisQueue) Peek() string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	task, err := rq.client.LIndex(ctx, rq.key, -1).Result()
	if err == redis.Nil {
		return ""
	}
	if err != nil {
		return ""
	}
	return task
}

// Len returns the number of tasks currently in the queue.
func (rq *RedisQueue) Len() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	count, err := rq.client.LLen(ctx, rq.key).Result()
	if err != nil {
		return 0
	}
	return int(count)
}

// Close releases the Redis connection.
func (rq *RedisQueue) Close() error {
	return rq.client.Close()
}

// ─── Redis Priority Queue (PriorityTaskQueue interface) ──────────────────────

// RedisPriorityQueue provides a Redis ZSET-backed distributed priority queue.
// Lower priority values execute first (Critical=0 before Background=4).
// Multiple processes can share a single RedisPriorityQueue.
//
// Uses:
//   - ZADD with priority score for enqueue
//   - ZPOPMIN for dequeue (lowest score = highest priority)
//   - ZRANGE 0 0 WITHSCORES for peek
//   - ZCARD for length
type RedisPriorityQueue struct {
	client *redis.Client
	key    string
}

// NewRedisPriorityQueue creates a Redis-backed priority queue.
//
// The addr should be a Redis connection string like "localhost:6379".
// The key is the Redis ZSET key used for storage.
// Pass "" for key to use the default "bt:queue:priority".
func NewRedisPriorityQueue(addr, key string) *RedisPriorityQueue {
	if key == "" {
		key = "bt:queue:priority"
	}
	return &RedisPriorityQueue{
		client: redis.NewClient(&redis.Options{Addr: addr}),
		key:    key,
	}
}

// NewRedisPriorityQueueFromClient creates a Redis-backed priority queue
// from an existing Redis client.
func NewRedisPriorityQueueFromClient(client *redis.Client, key string) *RedisPriorityQueue {
	if key == "" {
		key = "bt:queue:priority"
	}
	return &RedisPriorityQueue{client: client, key: key}
}

// Enqueue adds a task with the given priority and returns its unique ID.
// Uses priority value as the ZSET score and JSON-serialized PriorityTask as the member.
func (rpq *RedisPriorityQueue) Enqueue(task, agent string, priority Priority) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pt := PriorityTask{
		ID:       redisID(),
		Task:     task,
		Agent:    agent,
		Priority: priority,
		QueuedAt: time.Now(),
	}

	data, err := json.Marshal(pt)
	if err != nil {
		return ""
	}

	rpq.client.ZAdd(ctx, rpq.key, redis.Z{
		Score:  float64(priority),
		Member: string(data),
	})

	return pt.ID
}

// Dequeue removes and returns the highest-priority task.
// Returns an empty PriorityTask if the queue is empty.
func (rpq *RedisPriorityQueue) Dequeue() PriorityTask {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	members, err := rpq.client.ZPopMin(ctx, rpq.key, 1).Result()
	if err != nil || len(members) == 0 {
		return PriorityTask{}
	}

	var pt PriorityTask
	if err := json.Unmarshal([]byte(members[0].Member.(string)), &pt); err != nil {
		return PriorityTask{}
	}
	return pt
}

// Peek returns the highest-priority task without removing it.
// Returns an empty PriorityTask if the queue is empty.
func (rpq *RedisPriorityQueue) Peek() PriorityTask {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	members, err := rpq.client.ZRangeWithScores(ctx, rpq.key, 0, 0).Result()
	if err != nil || len(members) == 0 {
		return PriorityTask{}
	}

	var pt PriorityTask
	if err := json.Unmarshal([]byte(members[0].Member.(string)), &pt); err != nil {
		return PriorityTask{}
	}
	return pt
}

// Len returns the number of tasks currently in the queue.
func (rpq *RedisPriorityQueue) Len() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := rpq.client.ZCard(ctx, rpq.key).Result()
	if err != nil {
		return 0
	}
	return int(count)
}

// Purge removes all tasks from the queue.
func (rpq *RedisPriorityQueue) Purge() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rpq.client.Del(ctx, rpq.key)
}

// Close releases the Redis connection.
func (rpq *RedisPriorityQueue) Close() error {
	return rpq.client.Close()
}

// redisID generates a unique ID for priority queue tasks.
// Uses nanosecond timestamp to avoid collisions within the same process;
// Redis ZSET ensures uniqueness across processes (duplicate scores OK,
// duplicate members are updated in-place).
func redisID() string {
	return "rq-" + time.Now().Format("20060102150405.000000000")
}
