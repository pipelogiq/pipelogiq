package constants

import "fmt"

// ApplicationQueueID returns a stable application-specific queue prefix.
// It intentionally uses the numeric application id so stage routing stays
// deterministic without relying on any mutable app metadata.
func ApplicationQueueID(applicationID int) string {
	return fmt.Sprintf("app_%d", applicationID)
}

func StageNextQueueName(appID string, handler string) string {
	return appID + "_" + handler + "_" + StageNext
}
