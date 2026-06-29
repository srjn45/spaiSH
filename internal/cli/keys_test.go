package cli

import (
	"bytes"
	"io"
	"testing"

	"spaish/internal/agent"
)

func TestCycleMode(t *testing.T) {
	cases := []struct{ in, want string }{
		{agent.ModeManual, agent.ModeAuto},
		{agent.ModeAuto, agent.ModePlan},
		{agent.ModePlan, agent.ModeManual},
		{"bogus", agent.ModeManual},
	}
	for _, c := range cases {
		if got := cycleMode(c.in); got != c.want {
			t.Errorf("cycleMode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCycleModeFullRotation(t *testing.T) {
	m := agent.ModeManual
	for i := 0; i < 3; i++ {
		m = cycleMode(m)
	}
	if m != agent.ModeManual {
		t.Errorf("three cycles should return to manual, got %q", m)
	}
}

type readCloser struct{ io.Reader }

func (readCloser) Close() error { return nil }

func readAll(t *testing.T, src []byte) []byte {
	t.Helper()
	r := newShiftTabReader(readCloser{bytes.NewReader(src)})
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return out
}

func TestShiftTabReaderRewritesSequence(t *testing.T) {
	in := append([]byte("ab"), append(shiftTabSeq, []byte("cd")...)...)
	got := readAll(t, in)
	want := []byte{'a', 'b', shiftTabSentinel, 'c', 'd'}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestShiftTabReaderRewritesMultiple(t *testing.T) {
	in := append(append([]byte{}, shiftTabSeq...), shiftTabSeq...)
	got := readAll(t, in)
	want := []byte{shiftTabSentinel, shiftTabSentinel}
	if !bytes.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestShiftTabReaderPassesThroughOtherInput(t *testing.T) {
	// A plain line and an unrelated escape sequence (up arrow: ESC [ A) must
	// pass through untouched.
	in := []byte{'h', 'i', '\n', keyEsc, '[', 'A'}
	got := readAll(t, in)
	if !bytes.Equal(got, in) {
		t.Errorf("got %v, want %v", got, in)
	}
}
