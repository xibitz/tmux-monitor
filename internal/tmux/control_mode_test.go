package tmux

import (
	"bytes"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
)

func TestReadControlFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/control-mode.txt")
	if err != nil {
		t.Fatal(err)
	}
	var actual []ControlEvent
	if err := ReadControl(iotest.OneByteReader(bytes.NewReader(data)), func(event ControlEvent) {
		actual = append(actual, event)
	}); err != nil {
		t.Fatal(err)
	}
	want := []ControlEvent{
		{Kind: "inventory.changed"},
		{Kind: "pane.output", Pane: "%7", Data: []byte("hello\r\n\\")},
		{Kind: "command.error"},
		{Kind: "pane.output", Pane: "%7", Data: []byte("\x1b[32m")},
		{Kind: "client.state"},
	}
	if !reflect.DeepEqual(want, actual) {
		t.Fatalf("control events = %#v", actual)
	}
}

func TestReadControlTruncated(t *testing.T) {
	for _, value := range []string{"%output %7 a", "%begin 42 1 0\n%end 42 2 0\n", "%output %7 \\400\n", strings.Repeat("x", maxControlLine)} {
		if err := ReadControl(strings.NewReader(value), func(ControlEvent) {
			t.Fatal("malformed frame produced an event")
		}); !errors.Is(err, ErrControlFrame) {
			t.Fatalf("err = %v", err)
		}
	}
}

func FuzzReadControl(f *testing.F) {
	f.Add([]byte("hello"))
	f.Add([]byte("%window-close @2\n%begin 1 8 0\n\\\xff"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = ReadControl(bytes.NewReader(data), func(ControlEvent) {})
		if len(data) > 1024 {
			return
		}
		var payload strings.Builder
		for _, b := range data {
			payload.Write([]byte{'\\', '0' + (b >> 6), '0' + ((b >> 3) & 7), '0' + (b & 7)})
		}
		stream := "%begin 1 1 0\n%sessions-changed\n%end 1 1 0\n%output %4 " + payload.String() + "\n"
		var count int
		err := ReadControl(iotest.OneByteReader(strings.NewReader(stream)), func(event ControlEvent) {
			count++
			if event.Kind != "pane.output" || event.Pane != "%4" || !bytes.Equal(event.Data, data) {
				t.Fatalf("unexpected control event: %#v", event)
			}
		})
		if err != nil || count != 1 {
			t.Fatalf("count = %d, err = %v", count, err)
		}
	})
}
