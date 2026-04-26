package blocklist

import (
	"testing"
)

func TestParse_BasicEntries(t *testing.T) {
	data := []byte("0.0.0.0 ads.example.com\n0.0.0.0 tracker.io\n")
	result := parse(data)
	if _, ok := result["ads.example.com"]; !ok {
		t.Error("expected ads.example.com to be blocked")
	}
	if _, ok := result["tracker.io"]; !ok {
		t.Error("expected tracker.io to be blocked")
	}
	if len(result) != 2 {
		t.Errorf("expected 2 entries, got %d", len(result))
	}
}

func TestParse_IgnoresComments(t *testing.T) {
	data := []byte("# commentaire\n0.0.0.0 # pas de domaine\n0.0.0.0 valid.com\n")
	result := parse(data)
	if _, ok := result["valid.com"]; !ok {
		t.Error("expected valid.com to be blocked")
	}
	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
}

func TestParse_IgnoresLocalhost(t *testing.T) {
	data := []byte("127.0.0.1 localhost\n0.0.0.0 0.0.0.0\n0.0.0.0 broadcasthost\n0.0.0.0 ads.com\n")
	result := parse(data)
	if _, ok := result["localhost"]; ok {
		t.Error("localhost should not be blocked")
	}
	if _, ok := result["0.0.0.0"]; ok {
		t.Error("0.0.0.0 should not be blocked")
	}
	if _, ok := result["broadcasthost"]; ok {
		t.Error("broadcasthost should not be blocked")
	}
	if _, ok := result["ads.com"]; !ok {
		t.Error("expected ads.com to be blocked")
	}
}

func TestParse_EmptyInput(t *testing.T) {
	result := parse([]byte{})
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestParse_CRLFLineEndings(t *testing.T) {
	data := []byte("0.0.0.0 ads.example.com\r\n0.0.0.0 tracker.io\r\n")
	result := parse(data)
	if _, ok := result["ads.example.com"]; !ok {
		t.Error("expected ads.example.com to be blocked (CRLF)")
	}
	if _, ok := result["tracker.io"]; !ok {
		t.Error("expected tracker.io to be blocked (CRLF)")
	}
}

func TestParse_RealWorldSample(t *testing.T) {
	data := []byte(`# This hosts file is a merged collection of hosts from reputable sources,
# with a dash of crowd sourcing via Github

127.0.0.1 localhost
127.0.0.1 localhost.localdomain

# Start StevenBlack

0.0.0.0 0.0.0.0
0.0.0.0 ad.doubleclick.net
0.0.0.0 ads.google.com
0.0.0.0 tracker.io
`)
	result := parse(data)
	expected := []string{"ad.doubleclick.net", "ads.google.com", "tracker.io"}
	for _, d := range expected {
		if _, ok := result[d]; !ok {
			t.Errorf("expected %s to be blocked", d)
		}
	}
	notExpected := []string{"localhost", "0.0.0.0", "localhost.localdomain"}
	for _, d := range notExpected {
		if _, ok := result[d]; ok {
			t.Errorf("%s should not be blocked", d)
		}
	}
	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}
}
