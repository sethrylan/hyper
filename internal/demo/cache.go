// Package demo provides the embedded cache used by demo mode.
package demo

import _ "embed"

//go:embed cache.json
var cacheJSON string

// CacheJSON returns a copy of the demo cache contents.
func CacheJSON() []byte {
	return []byte(cacheJSON)
}
