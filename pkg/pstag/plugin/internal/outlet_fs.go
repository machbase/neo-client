package internal

import (
	"encoding/csv"
	"io"
	"log/slog"
	"os"
	"strconv"

	"github.com/machbase/neo-client/pkg/pstag/report"
)

func NewFileOutlet(args ...string) report.Outlet {
	return &FileOutlet{path: args[0]}
}

type FileOutlet struct {
	path   string
	w      *csv.Writer
	closer io.Closer
}

func (fo *FileOutlet) Open() error {
	var out io.Writer
	if fo.path == "-" {
		out = io.Writer(os.Stdout)
	} else {
		if f, err := os.OpenFile(fo.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err != nil {
			slog.Error("failed to open file", "path", fo.path, "error", err.Error())
		} else {
			out = f
			fo.closer = f
		}
	}
	fo.w = csv.NewWriter(out)
	return nil
}

func (fo *FileOutlet) Close() error {
	if fo.closer != nil {
		return fo.closer.Close()
	}
	return nil
}

func (fo *FileOutlet) Handle(recs []*report.Report) error {
	for _, r := range recs {
		strTs := strconv.FormatInt(r.Ts.Unix(), 10)
		for _, rec := range r.Records {
			strVal := strconv.FormatFloat(rec.Value, 'f', rec.Precision, 64)
			fo.w.Write([]string{rec.Name, strTs, strVal})
		}
	}
	fo.w.Flush()
	return nil
}
