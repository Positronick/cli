package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ReceiptName is the filename install.sh writes next to the binary —
// a cross-repo contract with install.sh and positronick.com/install.sh.
const ReceiptName = "positronick-install-receipt.json"

// Receipt records how the binary was installed. install.sh writes
// {"method":"installer","version":"..."}; `self update` rewrites it after a
// successful in-place update.
type Receipt struct {
	Method  string `json:"method"`
	Version string `json:"version"`
}

// ReadReceipt reads the receipt next to the resolved binary at binPath. A
// missing, unreadable or malformed receipt yields method "unknown" — the
// binary is then treated like a package-manager install, which `self update`
// must never replace in place.
func ReadReceipt(binPath string) Receipt {
	unknown := Receipt{Method: "unknown"}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(binPath), ReceiptName))
	if err != nil {
		return unknown
	}
	var r Receipt
	if err := json.Unmarshal(raw, &r); err != nil || r.Method == "" {
		return unknown
	}
	return r
}

// WriteReceipt writes the receipt next to the binary at binPath, in the same
// shape install.sh produces.
func WriteReceipt(binPath string, r Receipt) error {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encoding receipt: %w", err)
	}
	path := filepath.Join(filepath.Dir(binPath), ReceiptName)
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
