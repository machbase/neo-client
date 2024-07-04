package internal

import (
	"bytes"
	"encoding/csv"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/machbase/neo-client/pkg/pstag/report"
)

type HttpOutlet struct {
	addr   string
	client http.Client
}

func NewHttpOutlet(args ...string) report.Outlet {
	return &HttpOutlet{
		addr:   args[0],
		client: http.Client{},
	}
}

func (ho *HttpOutlet) Open() error {
	return nil
}

func (ho *HttpOutlet) Close() error {
	return nil
}

func (ho *HttpOutlet) Handle(recs []*report.Report) error {
	data := &bytes.Buffer{}

	w := csv.NewWriter(data)
	for _, r := range recs {
		strTs := strconv.FormatInt(r.Ts.Unix(), 10)
		for _, rec := range r.Records {
			strVal := strconv.FormatFloat(rec.Value, 'f', rec.Precision, 64)
			w.Write([]string{rec.Name, strTs, strVal})
		}
	}
	w.Flush()

	rsp, err := ho.client.Post(ho.addr, "text/csv", data)
	if err != nil {
		return err
	}
	defer rsp.Body.Close()
	body, err := io.ReadAll(rsp.Body)
	if err != nil {
		panic(err)
	}
	slog.Info("out-http", "status", rsp.Status, "response", string(body))
	return nil
}
