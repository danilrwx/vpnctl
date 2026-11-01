package cli

import (
	"fmt"
	"time"

	ucli "github.com/urfave/cli/v3"
)

// atFlag is the day expiry is measured against. The default is today in UTC:
// a host has no timezone the operator can reason about, and a date that depends
// on the laptop's zone would make the same state render two ways.
func atFlag() ucli.Flag {
	return &ucli.StringFlag{Name: "at", Usage: "YYYY-MM-DD, the day expiry is measured against; default today UTC"}
}

// dayArg is the --at flag as a time, or today in UTC.
func dayArg(c *ucli.Command) (time.Time, error) {
	s := c.String("at")
	if s == "" {
		return time.Now().UTC(), nil
	}

	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, Usage(fmt.Errorf("--at %q is not YYYY-MM-DD", s))
	}

	return t, nil
}
