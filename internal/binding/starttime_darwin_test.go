package binding

import (
	"os"
	"testing"
	"time"
)

func TestProcessStartTimeIsStableForThisProcess(t *testing.T) {
	pid := uint32(os.Getpid())
	a, err := processStartTime(pid)
	if err != nil {
		t.Fatal(err)
	}
	b, err := processStartTime(pid)
	if err != nil || a != b {
		t.Fatalf("start time changed: %+v %+v %v", a, b, err)
	}
	if at := time.Unix(a.Sec, a.Usec*1000); at.After(time.Now()) || time.Since(at) > 24*time.Hour || a.Usec < 0 || a.Usec >= 1_000_000 {
		t.Fatalf("implausible start time %+v", a)
	}
	if !Supported() {
		t.Fatal("darwin must support start times")
	}
}

func TestProcessStartTimeFailsForMissingProcess(t *testing.T) {
	if _, err := processStartTime(99999999); err == nil {
		t.Fatal("start time for a missing PID")
	}
}
