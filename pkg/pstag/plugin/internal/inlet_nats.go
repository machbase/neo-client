package internal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/machbase/neo-client/pkg/pstag/report"
	natsd "github.com/nats-io/nats-server/v2/server"
)

func NewNatsInlet(args ...string) report.Inlet {
	return &NatsInlet{
		Server:  args[0],
		Timeout: 5 * time.Second,
	}
}

type NatsInlet struct {
	Server  string
	Timeout time.Duration

	address *url.URL
	client  *http.Client
}

func (ni *NatsInlet) Open() error {
	address, err := url.Parse(ni.Server)
	if err != nil {
		return err
	}
	address.Path = path.Join(address.Path, "varz")
	ni.address = address
	return nil
}

func (ni *NatsInlet) Close() error {
	return nil
}

func (ni *NatsInlet) Handle() ([]*report.Record, error) {
	if ni.client == nil {
		timeout := ni.Timeout
		if timeout == time.Duration(0) {
			timeout = 5 * time.Second
		}
		ni.client = createHttpClient(timeout)
	}
	resp, err := ni.client.Get(ni.address.String())
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	stats := new(natsd.Varz)
	err = json.Unmarshal(data, stats)
	if err != nil {
		return nil, err
	}

	return []*report.Record{
		{Name: "nats.uptime", Value: float64((stats.Now.Sub(stats.Start)).Seconds()), Precision: 0},
		{Name: "nats.cores", Value: float64(stats.Cores), Precision: 0},
		{Name: "nats.cpu", Value: float64(stats.CPU), Precision: 0},
		{Name: "nats.mem", Value: float64(stats.Mem), Precision: 0},
		{Name: "nats.in_msgs", Value: float64(stats.InMsgs), Precision: 0},
		{Name: "nats.out_msgs", Value: float64(stats.OutMsgs), Precision: 0},
		{Name: "nats.in_bytes", Value: float64(stats.InBytes), Precision: 0},
		{Name: "nats.out_bytes", Value: float64(stats.OutBytes), Precision: 0},
		{Name: "nats.subscriptions", Value: float64(stats.Subscriptions), Precision: 0},
		{Name: "nats.slow_consumers", Value: float64(stats.SlowConsumers), Precision: 0},
		{Name: "nats.connections", Value: float64(stats.Connections), Precision: 0},
		{Name: "nats.total_connections", Value: float64(stats.TotalConnections), Precision: 0},
	}, nil
}

func createHttpClient(timeout time.Duration) *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}
}
