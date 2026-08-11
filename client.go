package neoclient

import (
	"context"

	"github.com/machbase/neo-client/machbase"
)

const Name = "machbase"

func NewAppender(ctx context.Context) *machbase.Appender {
	return machbase.NewAppender(ctx)
}
