package internal

import (
	"bytes"
	"encoding/csv"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
	"github.com/machbase/neo-client/pkg/pstag/report"
)

type MqttOutlet struct {
	addr    string
	host    string
	topic   string
	qos     byte
	timeout time.Duration
	client  paho.Client
}

func NewMqttOutlet(args ...string) report.Outlet {
	return &MqttOutlet{
		addr:    args[0],
		qos:     1,
		timeout: 3 * time.Second,
	}
}

func (ho *MqttOutlet) Open() error {
	address, err := url.Parse(ho.addr)
	if err != nil {
		return err
	}
	ho.host = address.Host
	ho.topic = strings.TrimPrefix(address.Path, "/")

	opts := paho.NewClientOptions()
	opts.SetCleanSession(true)
	opts.SetConnectRetry(false)
	opts.SetAutoReconnect(true)
	opts.SetProtocolVersion(4)
	opts.SetClientID("pstag")
	opts.AddBroker(ho.host)
	opts.SetKeepAlive(60 * time.Second)

	ho.client = paho.NewClient(opts)
	token := ho.client.Connect()
	token.WaitTimeout(ho.timeout)
	if token.Error() != nil {
		return token.Error()
	}
	return nil
}

func (ho *MqttOutlet) Close() error {
	if ho.client != nil {
		ho.client.Disconnect(1000)
	}
	return nil
}

func (ho *MqttOutlet) Handle(recs []*report.Report) error {
	data := &bytes.Buffer{}
	w := csv.NewWriter(data)
	for _, r := range recs {
		strTs := strconv.FormatInt(r.Ts.UnixNano(), 10)
		for _, rec := range r.Records {
			strVal := strconv.FormatFloat(rec.Value, 'f', rec.Precision, 64)
			w.Write([]string{rec.Name, strTs, strVal})
		}
	}
	w.Flush()

	tok := ho.client.Publish(ho.topic, ho.qos, false, data.Bytes())
	tok.WaitTimeout(ho.timeout)
	if tok.Error() != nil {
		slog.Error("out-mqtt", "error", tok.Error().Error())
	}
	return nil
}
