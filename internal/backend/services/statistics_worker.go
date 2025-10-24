package services

import (
	"log"
	"sync"

	webuimodels "notificator/internal/webui/models"
)

// StatisticsCaptureJob represents a statistics capture job
type StatisticsCaptureJob struct {
	JobType     string // "fired", "resolved", "acknowledged"
	Fingerprint string
	AlertName   string
	Severity    string
	Data        map[string]interface{} // Minimal data needed for capture
}

// StatisticsWorkerPool manages a pool of workers for capturing statistics
type StatisticsWorkerPool struct {
	captureService *StatisticsCaptureService
	jobs           chan *StatisticsCaptureJob
	workerCount    int
	wg             sync.WaitGroup
	stopChan       chan struct{}
}

// NewStatisticsWorkerPool creates a new worker pool
func NewStatisticsWorkerPool(captureService *StatisticsCaptureService, workerCount int, queueSize int) *StatisticsWorkerPool {
	return &StatisticsWorkerPool{
		captureService: captureService,
		jobs:           make(chan *StatisticsCaptureJob, queueSize),
		workerCount:    workerCount,
		stopChan:       make(chan struct{}),
	}
}

// Start starts the worker pool
func (swp *StatisticsWorkerPool) Start() {
	for i := 0; i < swp.workerCount; i++ {
		swp.wg.Add(1)
		go swp.worker(i)
	}
	log.Printf("📊 Started statistics worker pool with %d workers", swp.workerCount)
}

// Stop stops the worker pool gracefully
func (swp *StatisticsWorkerPool) Stop() {
	close(swp.stopChan)
	close(swp.jobs)
	swp.wg.Wait()
	log.Printf("📊 Stopped statistics worker pool")
}

// Submit submits a job to the worker pool (non-blocking)
func (swp *StatisticsWorkerPool) Submit(job *StatisticsCaptureJob) {
	select {
	case swp.jobs <- job:
		// Job submitted successfully
	case <-swp.stopChan:
		// Worker pool is stopping, drop job
	default:
		// Queue is full, drop job to prevent blocking
		log.Printf("⚠️  Statistics worker queue full, dropping job for %s", job.Fingerprint)
	}
}

// SubmitAlertFired submits an alert fired job with minimal data
func (swp *StatisticsWorkerPool) SubmitAlertFired(alert *webuimodels.DashboardAlert) {
	job := &StatisticsCaptureJob{
		JobType:     "fired",
		Fingerprint: alert.Fingerprint,
		AlertName:   alert.AlertName,
		Severity:    alert.Severity,
		Data: map[string]interface{}{
			"alert": alert, // Store the full alert pointer
		},
	}
	swp.Submit(job)
}

// SubmitAlertResolved submits an alert resolved job with minimal data
func (swp *StatisticsWorkerPool) SubmitAlertResolved(fingerprint string, resolvedAt interface{}) {
	job := &StatisticsCaptureJob{
		JobType:     "resolved",
		Fingerprint: fingerprint,
		Data: map[string]interface{}{
			"resolved_at": resolvedAt,
		},
	}
	swp.Submit(job)
}

// SubmitAlertAcknowledged submits an alert acknowledged job with minimal data
func (swp *StatisticsWorkerPool) SubmitAlertAcknowledged(fingerprint string, acknowledgedAt interface{}) {
	job := &StatisticsCaptureJob{
		JobType:     "acknowledged",
		Fingerprint: fingerprint,
		Data: map[string]interface{}{
			"acknowledged_at": acknowledgedAt,
		},
	}
	swp.Submit(job)
}

// worker processes jobs from the queue
func (swp *StatisticsWorkerPool) worker(id int) {
	defer swp.wg.Done()

	for {
		select {
		case job, ok := <-swp.jobs:
			if !ok {
				// Channel closed, worker should exit
				return
			}
			swp.processJob(job)
		case <-swp.stopChan:
			return
		}
	}
}

// processJob processes a single statistics capture job
func (swp *StatisticsWorkerPool) processJob(job *StatisticsCaptureJob) {
	switch job.JobType {
	case "fired":
		// Convert minimal data back to alert
		alert := swp.reconstructAlertForFired(job)
		if err := swp.captureService.CaptureAlertFired(alert); err != nil {
			log.Printf("Failed to capture alert fired statistics for %s: %v", job.Fingerprint, err)
		}

	case "resolved":
		// Update resolved status
		if err := swp.captureService.UpdateAlertResolvedMinimal(job.Fingerprint, job.Data["resolved_at"]); err != nil {
			log.Printf("Failed to update alert resolved statistics for %s: %v", job.Fingerprint, err)
		}

	case "acknowledged":
		// Update acknowledged status
		if err := swp.captureService.UpdateAlertAcknowledgedMinimal(job.Fingerprint, job.Data["acknowledged_at"]); err != nil {
			log.Printf("Failed to update alert acknowledged statistics for %s: %v", job.Fingerprint, err)
		}
	}
}

// reconstructAlertForFired reconstructs minimal alert data for capture
func (swp *StatisticsWorkerPool) reconstructAlertForFired(job *StatisticsCaptureJob) *webuimodels.DashboardAlert {
	// Store full alert in job data instead of reconstructing
	if alert, ok := job.Data["alert"].(*webuimodels.DashboardAlert); ok {
		return alert
	}

	// Fallback: return empty alert (shouldn't happen)
	return &webuimodels.DashboardAlert{
		Fingerprint: job.Fingerprint,
		AlertName:   job.AlertName,
		Severity:    job.Severity,
	}
}
