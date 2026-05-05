package logsec

import (
	"bytes"
	"strings"
	"testing"
)

func TestScrubLine_LinuxHome(t *testing.T) {
	got := ScrubLine("open /home/akerimus/.config/levoile/config.toml: denied")
	if !strings.Contains(got, "$HOME/.config/levoile") {
		t.Fatalf("scrubbed line should collapse /home/<user>; got %q", got)
	}
	if strings.Contains(got, "akerimus") {
		t.Fatalf("scrubbed line still contains user name: %q", got)
	}
}

func TestScrubLine_WindowsProfile(t *testing.T) {
	got := ScrubLine(`open C:\Users\Akerimus\AppData\Roaming\LeVoile\config.toml: denied`)
	if !strings.Contains(got, `$HOME\AppData\Roaming\LeVoile`) {
		t.Fatalf("scrubbed line should collapse C:\\Users\\<user>; got %q", got)
	}
	if strings.Contains(got, "Akerimus") {
		t.Fatalf("scrubbed line still contains user name: %q", got)
	}
}

func TestScrubLine_RootHome(t *testing.T) {
	got := ScrubLine("read /root/.local/state/levoile.lock: permission denied")
	if !strings.Contains(got, "$HOME/.local/state") {
		t.Fatalf("scrubbed line should collapse /root; got %q", got)
	}
}

func TestScrubLine_NoHome(t *testing.T) {
	in := "firewall: deactivate: nft delete table failed"
	if got := ScrubLine(in); got != in {
		t.Fatalf("untouched lines must round-trip; got %q", got)
	}
}

func TestScrubLine_Empty(t *testing.T) {
	if got := ScrubLine(""); got != "" {
		t.Fatalf("empty input must return empty; got %q", got)
	}
}

func TestNewWriter_PassesThroughUnchanged(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	in := []byte("plain log entry without PII\n")
	n, err := w.Write(in)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(in) {
		t.Fatalf("short write: %d != %d", n, len(in))
	}
	if buf.String() != string(in) {
		t.Fatalf("payload changed: %q != %q", buf.String(), string(in))
	}
}

func TestNewWriter_ScrubsHomeOnTheFly(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf)
	in := []byte("open /home/akerimus/file: nope\n")
	n, err := w.Write(in)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if n != len(in) {
		t.Fatalf("reported length must match the input length; got %d != %d", n, len(in))
	}
	if strings.Contains(buf.String(), "akerimus") {
		t.Fatalf("scrubbed writer leaked user name: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "$HOME") {
		t.Fatalf("scrubbed writer dropped the placeholder: %q", buf.String())
	}
}
