package worker

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// JobStatus values.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// Job represents an async ingest task.
type Job struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	Status      string    `json:"status"`
	DocsIndexed int64     `json:"docs_indexed"`
	DocsTotal   int64     `json:"docs_total"`
	Error       string    `json:"error,omitempty"`
	StartedAt   time.Time `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
	SubmittedBy string    `json:"submitted_by"`
}

// Queue manages a pool of ingest workers and tracks job state.
type Queue struct {
	mu       sync.RWMutex
	jobs     map[string]*Job
	taskCh   chan *Task
	counter  atomic.Int64
	workerN  int
}

// Task is the internal work unit sent to a worker goroutine.
type Task struct {
	Job      *Job
	FilePath string
	OnDone   func(job *Job) // called when job completes (triggers search-service reindex)
}

// New creates a Queue and starts n background workers.
func New(workerCount int) *Queue {
	q := &Queue{
		jobs:    make(map[string]*Job),
		taskCh:  make(chan *Task, 100),
		workerN: workerCount,
	}
	for i := 0; i < workerCount; i++ {
		go q.worker(i)
	}
	slog.Info("ingest worker pool started", "workers", workerCount)
	return q
}

// Submit enqueues a new ingest job and returns the job ID.
func (q *Queue) Submit(filename, filePath, submittedBy string, onDone func(job *Job)) string {
	id := fmt.Sprintf("job-%d", q.counter.Add(1))
	job := &Job{
		ID:          id,
		Filename:    filename,
		Status:      StatusPending,
		StartedAt:   time.Now(),
		SubmittedBy: submittedBy,
	}
	q.mu.Lock()
	q.jobs[id] = job
	q.mu.Unlock()

	q.taskCh <- &Task{Job: job, FilePath: filePath, OnDone: onDone}
	slog.Info("job submitted", "job_id", id, "file", filename, "by", submittedBy)
	return id
}

// GetJob returns a job by ID.
func (q *Queue) GetJob(id string) (*Job, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	j, ok := q.jobs[id]
	return j, ok
}

// ListJobs returns all jobs, newest first.
func (q *Queue) ListJobs() []*Job {
	q.mu.RLock()
	defer q.mu.RUnlock()
	out := make([]*Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, j)
	}
	return out
}

// worker is the background goroutine that processes tasks.
func (q *Queue) worker(id int) {
	slog.Info("ingest worker started", "worker_id", id)
	for task := range q.taskCh {
		q.process(task)
	}
}

// process executes a single ingest task.
func (q *Queue) process(task *Task) {
	job := task.Job
	q.setStatus(job.ID, StatusProcessing)
	slog.Info("processing job", "job_id", job.ID, "file", job.Filename)

	// Import parquet reader inline to keep worker package dependency-free.
	docs, err := readParquetFile(task.FilePath)
	if err != nil {
		now := time.Now()
		q.mu.Lock()
		job.Status = StatusFailed
		job.Error = err.Error()
		job.FinishedAt = &now
		q.mu.Unlock()
		slog.Error("job failed", "job_id", job.ID, "err", err)
		return
	}

	now := time.Now()
	q.mu.Lock()
	job.Status = StatusDone
	job.DocsIndexed = int64(len(docs))
	job.DocsTotal = int64(len(docs))
	job.FinishedAt = &now
	q.mu.Unlock()

	slog.Info("job complete", "job_id", job.ID, "docs", len(docs))

	if task.OnDone != nil {
		task.OnDone(job)
	}
}

func (q *Queue) setStatus(id, status string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if j, ok := q.jobs[id]; ok {
		j.Status = status
	}
}
