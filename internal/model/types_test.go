package model

import (
	"bytes"
	"encoding/binary"
	"testing"
	"unsafe"
)

// TestRawEventLayout asserts that the Go struct has the exact wire layout of
// struct event in bpf/goodman.h: no implicit padding, same total size.
func TestRawEventLayout(t *testing.T) {
	var e RawEvent
	if got := unsafe.Sizeof(e); got != RawEventSize {
		t.Fatalf("RawEvent size = %d, want %d (implicit padding drift vs bpf/goodman.h)", got, RawEventSize)
	}
	offsets := map[string]uintptr{
		"PID":       unsafe.Offsetof(e.PID),
		"TID":       unsafe.Offsetof(e.TID),
		"DirFD":     unsafe.Offsetof(e.DirFD),
		"Type":      unsafe.Offsetof(e.Type),
		"Comm":      unsafe.Offsetof(e.Comm),
		"Arg":       unsafe.Offsetof(e.Arg),
		"Pad":       unsafe.Offsetof(e.Pad),
		"StackLen":  unsafe.Offsetof(e.StackLen),
		"StackPad":  unsafe.Offsetof(e.StackPad),
		"Stack":     unsafe.Offsetof(e.Stack),
		"Timestamp": unsafe.Offsetof(e.Timestamp),
	}
	want := map[string]uintptr{
		"PID": 0, "TID": 4, "DirFD": 8, "Type": 12, "Comm": 13, "Arg": 29,
		"Pad": 285, "StackLen": 288, "StackPad": 292, "Stack": 296, "Timestamp": 552,
	}
	for f, w := range want {
		if offsets[f] != w {
			t.Errorf("offset of %s = %d, want %d", f, offsets[f], w)
		}
	}
}

// TestRawEventRoundTrip round-trips a RawEvent through encoding/binary.
func TestRawEventRoundTrip(t *testing.T) {
	in := RawEvent{PID: 1234, TID: 5678, DirFD: ATFDCWD, Type: uint8(EventNetConnect), StackLen: 3, Timestamp: 99}
	copy(in.Comm[:], "node")
	copy(in.Arg[:], "1.2.3.4:443")
	in.Stack[0], in.Stack[1], in.Stack[2] = 0xdead, 0xbeef, 0xcafe

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, &in); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != RawEventSize {
		t.Fatalf("encoded size = %d, want %d", buf.Len(), RawEventSize)
	}
	var out RawEvent
	if err := binary.Read(&buf, binary.LittleEndian, &out); err != nil {
		t.Fatal(err)
	}
	if out != in {
		t.Fatalf("round-trip drift:\n in=%+v\nout=%+v", in, out)
	}
	if out.CommString() != "node" || out.ArgString() != "1.2.3.4:443" {
		t.Fatalf("string accessors broken: %q %q", out.CommString(), out.ArgString())
	}
	if len(out.UserStack()) != 3 {
		t.Fatalf("UserStack len = %d, want 3", len(out.UserStack()))
	}
}
