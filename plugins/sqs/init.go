package sqs

import (
	"github.com/bytefreezer/proxy/plugins"
)

func init() {
	plugins.Register("sqs", NewSQSPlugin)
}
