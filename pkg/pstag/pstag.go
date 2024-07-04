package pstag

import (
	"log/slog"
	"sync"
	"time"

	"github.com/machbase/neo-client/pkg/pstag/report"
)

func New(opts ...Option) *PsTag {
	ret := &PsTag{}
	for _, opt := range opts {
		opt(ret)
	}
	if ret.reportCh == nil {
		ret.reportCh = make(chan *report.Report, 100)
		ret.shouldCloseReportCh = true
	}
	return ret
}

type Option func(*PsTag)

func WithInterval(interval time.Duration) Option {
	return func(pt *PsTag) {
		pt.interval = interval
	}
}

func WithTagPrefix(prefix string) Option {
	return func(pt *PsTag) {
		pt.tagPrefix = prefix
	}
}

func WithReportCh(ch chan *report.Report) Option {
	return func(pt *PsTag) {
		pt.reportCh = ch
	}
}

type PsTag struct {
	interval            time.Duration
	tagPrefix           string
	inputs              []*InputHandler
	outputs             []*OutputHandler
	reportCh            chan *report.Report
	shouldCloseReportCh bool

	closeCh chan bool
	closeWg sync.WaitGroup
}

func (pt *PsTag) AddInput(inlet report.Inlet) {
	pt.inputs = append(pt.inputs, NewInputFunc(pt.reportCh, inlet))
}

func (pt *PsTag) AddOutput(outlet report.Outlet) {
	pt.outputs = append(pt.outputs, NewOutputHandler(outlet, pt.interval))
}

func (pt *PsTag) Run() {
	if pt.interval < 1*time.Second {
		pt.interval = 1 * time.Second
	}

	slog.Info("set", "interval", pt.interval, "inputs", len(pt.inputs), "outputs", len(pt.outputs))

	for _, out := range pt.outputs {
		out.Start()
	}
	for _, in := range pt.inputs {
		in.Start(pt.interval, pt.tagPrefix)
	}

	pt.closeCh = make(chan bool)
	pt.closeWg.Add(1)
	go func() {
		slog.Info("start")
		for {
			select {
			case <-pt.closeCh:
				pt.closeWg.Done()
				return
			case rpt := <-pt.reportCh:
				for _, out := range pt.outputs {
					out.Sink() <- rpt
				}
			}
		}
	}()
}

func (pt *PsTag) Stop() {
	for _, in := range pt.inputs {
		in.Stop()
	}
	for _, out := range pt.outputs {
		out.Stop()
	}
	pt.closeCh <- true
	pt.closeWg.Wait()

	if pt.shouldCloseReportCh {
		close(pt.reportCh)
	}
	slog.Info("stopped")
}
