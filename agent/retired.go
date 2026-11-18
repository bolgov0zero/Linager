package agent

import (
	"os"
	"path/filepath"
)

// retiredFiles are files in the data directory that past releases wrote and the current one does not use.
var retiredFiles = []string{"gateway.json", "gateway.json.tmp"}

// RemoveRetiredData deletes what a retired module left in the data directory and returns the names it removed.
func RemoveRetiredData(dir string) []string {
	var removed []string
	for _, name := range retiredFiles {
		if os.Remove(filepath.Join(dir, name)) == nil {
			removed = append(removed, name)
		}
	}
	return removed
}
