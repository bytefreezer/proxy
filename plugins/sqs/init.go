package sqs

import (
	"github.com/n0needt0/bytefreezer-proxy/plugins"
)

func init() {
	plugins.Register("sqs", NewSQSPlugin)
}