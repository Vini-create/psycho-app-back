package email

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type WorkerConfig struct {
	Concurrency  int
	PollInterval time.Duration
}

type Worker struct {
	service *Service
	config  WorkerConfig
}

func NewWorker(service *Service, config WorkerConfig) (*Worker, error) {
	if service == nil || config.Concurrency < 1 || config.Concurrency > 16 ||
		config.PollInterval < 100*time.Millisecond || config.PollInterval > time.Minute {
		return nil, fmt.Errorf("email worker configuration is invalid")
	}
	return &Worker{service: service, config: config}, nil
}

func (w *Worker) Run(ctx context.Context) {
	var group sync.WaitGroup
	group.Add(w.config.Concurrency)
	for workerID := 1; workerID <= w.config.Concurrency; workerID++ {
		go func(id int) {
			defer group.Done()
			w.runOne(ctx, id)
		}(workerID)
	}
	group.Wait()
}

func (w *Worker) runOne(ctx context.Context, workerID int) {
	for {
		processed, err := w.service.ProcessNext(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Error("email worker iteration failed", "worker_id", workerID, "error", err)
		}
		if processed {
			continue
		}
		timer := time.NewTimer(w.config.PollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}
