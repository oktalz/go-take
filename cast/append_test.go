package cast

import "testing"

func TestAppend(t *testing.T) {
	a := mustRead(t, `{"version": 3, "term": {"cols": 80, "rows": 24}}
[0.5, "o", "$ one\r\n"]
[0.2, "m", "first"]
[0.3, "c", "stage one"]
[1.0, "x", "0"]
`)
	b := mustRead(t, `{"version": 3, "term": {"cols": 80, "rows": 24}}
[2.0, "o", "$ two\r\n"]
[0.4, "m", "second"]
`)
	if err := a.Append(b, AppendOptions{Pause: 0.5, Clear: true}); err != nil {
		t.Fatal(err)
	}
	want := []Event{
		{Code: Output, Data: "$ one\r\n", Delay: 0.5},
		{Code: Marker, Data: "first", Delay: 0.2},
		{Code: Caption, Data: "stage one", Delay: 0.3},
		{Code: Caption, Delay: 0.5},
		{Code: Output, Data: ClearScreen},
		{Code: Output, Data: "$ two\r\n"},
		{Code: Marker, Data: "second", Delay: 0.4},
	}
	if len(a.Events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(a.Events), len(want), a.Events)
	}
	for i, e := range want {
		if a.Events[i] != e {
			t.Errorf("event %d: got %+v, want %+v", i, a.Events[i], e)
		}
	}
	if mt, err := a.MarkerTime("second"); err != nil || !near(mt, 1.9) {
		t.Errorf("marker second at %v (%v), want 1.9", mt, err)
	}
	if len(b.Events) != 2 || b.Events[0].Delay != 2.0 {
		t.Errorf("appended recording was modified: %+v", b.Events)
	}
}

func TestAppendSizeMismatch(t *testing.T) {
	a := mustRead(t, `{"version": 3, "term": {"cols": 80, "rows": 24}}
[0.5, "o", "a"]
`)
	b := mustRead(t, `{"version": 3, "term": {"cols": 100, "rows": 30}}
[0.5, "o", "b"]
`)
	if err := a.Append(b, AppendOptions{}); err == nil {
		t.Fatal("expected a size mismatch error")
	}
	a.Events = append(a.Events, Event{Code: Resize, Data: "100x30"})
	if err := a.Append(b, AppendOptions{}); err != nil {
		t.Fatalf("after resize to 100x30: %v", err)
	}
}
