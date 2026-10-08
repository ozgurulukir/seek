package parserdef

import "errors"

// ErrUnavailable identifies an absent auto-discovered source. It never
// identifies unreadable files, invalid data, or an unsupported version.
var ErrUnavailable = errors.New("source unavailable")
