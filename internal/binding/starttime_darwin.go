package binding

import (
	"errors"

	"golang.org/x/sys/unix"
)

const startTimeSupported = true

func processStartTime(pid uint32) (StartTime, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", int(pid))
	if err != nil {
		return StartTime{}, err
	}
	if uint32(info.Proc.P_pid) != pid {
		return StartTime{}, errors.New("process lookup returned another PID")
	}
	start := info.Proc.P_starttime
	if start.Sec <= 0 {
		return StartTime{}, errors.New("process start time unavailable")
	}
	return StartTime{Sec: start.Sec, Usec: int64(start.Usec)}, nil
}
