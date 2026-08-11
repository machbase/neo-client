package neoclient

import (
	"github.com/machbase/neo-client/machbase"
)

const Name = "machbase"

func NewAppender() *machbase.Appender {
	return &machbase.Appender{}
}
