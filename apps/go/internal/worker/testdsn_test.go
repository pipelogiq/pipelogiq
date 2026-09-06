package worker

import (
	"fmt"
	"sync/atomic"
)

// testDSNCounter makes in-memory database names unique across parallel tests.
var testDSNCounter atomic.Uint64

func testDSN(prefix string) string {
	return fmt.Sprintf(
		"file:%s_%d?mode=memory&cache=shared&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)",
		prefix,
		testDSNCounter.Add(1),
	)
}
