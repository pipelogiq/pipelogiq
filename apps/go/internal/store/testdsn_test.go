package store

import (
	"fmt"
	"sync/atomic"
)

// testDSNCounter makes in-memory database names unique. time.Now().UnixNano()
// alone is not enough: parallel tests starting within the same clock tick used
// to share a shared-cache database and then fail on "table already exists".
var testDSNCounter atomic.Uint64

// testDSN returns a private shared-cache SQLite DSN for one test.
func testDSN(prefix string) string {
	return fmt.Sprintf(
		"file:%s_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		prefix,
		testDSNCounter.Add(1),
	)
}
