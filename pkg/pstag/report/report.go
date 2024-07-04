package report

import "time"

type Inlet interface {
	Handle() ([]*Record, error)
	Open() error
	Close() error
}

type Outlet interface {
	Handle(r []*Report) error
	Open() error
	Close() error
}

type Report struct {
	Ts      time.Time `json:"ts"`
	Records []*Record `json:"records,omitempty"`
}

type Record struct {
	Name      string  `json:"name"`
	Value     float64 `json:"value"`
	Precision int     `json:"prec,omitempty"`
}
