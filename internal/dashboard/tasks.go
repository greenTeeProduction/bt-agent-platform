package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Task represents a workflow task in the pipeline.
type Task struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"` // critical, high, medium, low
	Status      string `json:"status"`   // pending, approved, rejected, in_progress, completed, failed
	Assignee    string `json:"assignee"` // agent name or role
	Sprint      int    `json:"sprint"`
	StoryPoints int    `json:"sp"`
	Source      string `json:"source"`    // thinktank, manual
	SourceID    string `json:"source_id"` // thinktank finding ID
	TreeID      string `json:"tree_id"`   // which BT tree to run
	CreatedAt   string `json:"created_at"`
	CompletedAt string `json:"completed_at,omitempty"`
	Output      string `json:"output,omitempty"`
	Outcome     string `json:"outcome,omitempty"`
}

// TaskStore persists tasks to a JSON file.
type TaskStore struct {
	mu    sync.Mutex
	path  string
	Tasks []Task `json:"tasks"`
}

func NewTaskStore(path string) *TaskStore {
	ts := &TaskStore{path: path, Tasks: []Task{}}
	ts.Load()
	return ts
}

func (s *TaskStore) Load() {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err != nil {
		return // fresh store, no tasks yet
	}
	json.Unmarshal(data, s)
}

func (s *TaskStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *TaskStore) saveLocked() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *TaskStore) List() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Task, len(s.Tasks))
	copy(out, s.Tasks)
	return out
}

func (s *TaskStore) Get(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func (s *TaskStore) Create(task Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.CreatedAt = time.Now().Format(time.RFC3339)
	if task.Status == "" {
		task.Status = "pending"
	}
	if task.Sprint == 0 {
		task.Sprint = 1
	}
	s.Tasks = append(s.Tasks, task)
	return s.saveLocked()
}

func (s *TaskStore) UpdateStatus(id, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Status = status
			if status == "completed" || status == "failed" {
				s.Tasks[i].CompletedAt = time.Now().Format(time.RFC3339)
			}
			return s.saveLocked()
		}
	}
	return fmt.Errorf("task %s not found", id)
}

func (s *TaskStore) SetOutput(id, output, outcome string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Tasks {
		if s.Tasks[i].ID == id {
			s.Tasks[i].Output = output
			s.Tasks[i].Outcome = outcome
			return s.saveLocked()
		}
	}
	return fmt.Errorf("task %s not found", id)
}

// Approved returns tasks with status "approved".
func (s *TaskStore) Approved() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.Tasks {
		if t.Status == "approved" {
			out = append(out, t)
		}
	}
	return out
}
