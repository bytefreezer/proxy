// Licensed under Elastic License 2.0
// See LICENSE.txt for details

package sqs

import (
	"github.com/bytefreezer/proxy/plugins"
)

func init() {
	plugins.Register("sqs", NewSQSPlugin)
}
