package pstag

import (
	"log/slog"
	"sync"
	"time"

	"github.com/machbase/neo-client/pkg/pstag/report"
)

type OutputHandler struct {
	ch      chan *report.Report
	outlet  report.Outlet
	closeCh chan bool
	closeWg sync.WaitGroup

	buffer        []*report.Report
	bufferTimeout time.Duration
}

func NewOutputHandler(outlet report.Outlet, interval time.Duration) *OutputHandler {
	return &OutputHandler{
		ch:            make(chan *report.Report, 1),
		outlet:        outlet,
		closeCh:       make(chan bool),
		buffer:        make([]*report.Report, 0, 256),
		bufferTimeout: interval,
	}
}

func (out *OutputHandler) Start() error {
	if err := out.outlet.Open(); err != nil {
		slog.Error("failed to open output", "error", err.Error())
		return err
	}

	tick := time.NewTicker(out.bufferTimeout)

	out.closeWg.Add(1)
	go func() {
	loop:
		for {
			select {
			case r := <-out.ch:
				out.buffer = append(out.buffer, r)
			case <-tick.C:
				out.flush()
			case <-out.closeCh:
				out.flush()
				break loop
			}
		}
		out.closeWg.Done()
	}()
	return nil
}

func (out *OutputHandler) flush() {
	if len(out.buffer) == 0 {
		return
	}
	if err := out.outlet.Handle(out.buffer); err != nil {
		slog.Error("failed to output flush", "error", err.Error())
	}
	out.buffer = out.buffer[:0]
}

func (out *OutputHandler) Stop() {
	out.closeCh <- true
	out.closeWg.Wait()
	close(out.ch)

	if err := out.outlet.Close(); err != nil {
		slog.Error("failed to open output", "error", err.Error())
	}
}

func (out *OutputHandler) Sink() chan<- *report.Report {
	return out.ch
}
