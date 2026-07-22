// Ring-buffer decoding and kernel-clock conversion.
package loader

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hi-heisenbug/goodman/internal/model"
	"golang.org/x/sys/unix"
)

// Read blocks for the next raw event from the kernel.
func (l *Loader) Read() (*model.RawEvent, error) {
	for {
		rec, err := l.reader.Read()
		if err != nil {
			return nil, err
		}
		ev, err := decodeRawEvent(rec.RawSample)
		if err != nil {
			l.discarded.Add(1)
			continue
		}
		return ev, nil
	}
}

func decodeRawEvent(sample []byte) (*model.RawEvent, error) {
	if len(sample) < model.RawEventSize {
		return nil, fmt.Errorf("short ring-buffer record: %d < %d", len(sample), model.RawEventSize)
	}
	var ev model.RawEvent
	if err := binary.Read(bytes.NewReader(sample[:model.RawEventSize]), binary.LittleEndian, &ev); err != nil {
		return nil, fmt.Errorf("decode ring-buffer record: %w", err)
	}
	return &ev, nil
}

// BootToUnixNs returns the offset that converts bpf_ktime_get_ns()
// (CLOCK_MONOTONIC since boot) into unix nanoseconds.
func BootToUnixNs() uint64 {
	var mono int64
	f, err := os.ReadFile("/proc/uptime")
	if err == nil {
		fields := strings.Fields(string(f))
		if len(fields) > 0 {
			if up, err := strconv.ParseFloat(fields[0], 64); err == nil {
				mono = int64(up * 1e9)
			}
		}
	}
	return uint64(time.Now().UnixNano() - mono)
}

// MonotonicNowNs returns CLOCK_MONOTONIC nanoseconds (matches bpf_ktime_get_ns).
func MonotonicNowNs() uint64 {
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &ts); err != nil {
		return uint64(time.Now().UnixNano())
	}
	return uint64(ts.Sec)*1e9 + uint64(ts.Nsec)
}
