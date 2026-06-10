package install

import (
	"os"
	"path/filepath"
	"testing"
)

// The receipt is how `self update` knows the binary was placed by install.sh
// (the only method it may replace in place). Anything else — missing file,
// unreadable file, malformed JSON — must degrade to "unknown" so package-
// manager installs are never clobbered.
func TestReadReceipt(t *testing.T) {
	binPath := func(t *testing.T, receipt string) string {
		t.Helper()
		dir := t.TempDir()
		bin := filepath.Join(dir, "positronick")
		if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if receipt != "" {
			if err := os.WriteFile(filepath.Join(dir, ReceiptName), []byte(receipt), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return bin
	}

	t.Run("installer receipt", func(t *testing.T) {
		bin := binPath(t, `{"method":"installer","version":"0.1.0"}`)
		got := ReadReceipt(bin)
		if got.Method != "installer" || got.Version != "0.1.0" {
			t.Errorf("ReadReceipt = %+v, want installer/0.1.0", got)
		}
	})

	t.Run("missing receipt means unknown", func(t *testing.T) {
		bin := binPath(t, "")
		if got := ReadReceipt(bin); got.Method != "unknown" {
			t.Errorf("ReadReceipt = %+v, want method unknown", got)
		}
	})

	t.Run("malformed receipt means unknown", func(t *testing.T) {
		bin := binPath(t, "not json at all")
		if got := ReadReceipt(bin); got.Method != "unknown" {
			t.Errorf("ReadReceipt = %+v, want method unknown", got)
		}
	})

	t.Run("empty method means unknown", func(t *testing.T) {
		bin := binPath(t, `{"version":"0.1.0"}`)
		if got := ReadReceipt(bin); got.Method != "unknown" {
			t.Errorf("ReadReceipt = %+v, want method unknown", got)
		}
	})
}

// WriteReceipt is the post-update bookkeeping: the next `self update` must
// see the new version, in the exact shape install.sh writes.
func TestWriteReceiptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "positronick")
	if err := os.WriteFile(bin, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteReceipt(bin, Receipt{Method: "installer", Version: "0.2.0"}); err != nil {
		t.Fatalf("WriteReceipt: %v", err)
	}
	got := ReadReceipt(bin)
	if got.Method != "installer" || got.Version != "0.2.0" {
		t.Errorf("round trip = %+v, want installer/0.2.0", got)
	}
}
