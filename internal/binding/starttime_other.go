//go:build !darwin

package binding

const startTimeSupported = false

func processStartTime(uint32) (StartTime, error) { return StartTime{}, errStartTimeUnsupported }
