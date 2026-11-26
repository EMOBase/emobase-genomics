// Package blastdb removes BLAST database files. makeblastdb writes each
// assembly's databases as {dbPath}/{AssemblyID}-{genome,protein,rna} plus its
// own extensions (.nsq, .nin, ...), where AssemblyID is "v{versionID}a{id}".
package blastdb

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// RemoveAssembly removes every BLAST database file of one assembly and returns
// how many it removed. The "-" after the AssemblyID keeps "v1a7" from
// matching "v1a70".
func RemoveAssembly(dbPath, assemblyID string) (int, error) {
	return remove(filepath.Join(dbPath, assemblyID+"-*"))
}

// RemoveVersion removes every BLAST database file of a Version's assemblies
// and returns how many it removed. The "a" after the version ID keeps "v1"
// from matching "v12".
func RemoveVersion(dbPath string, versionID uint64) (int, error) {
	return remove(filepath.Join(dbPath, fmt.Sprintf("v%da*", versionID)))
}

func remove(pattern string) (int, error) {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return 0, err
	}
	removed := 0
	var errs []error
	for _, m := range matches {
		if err := os.Remove(m); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
			continue
		}
		removed++
	}
	return removed, errors.Join(errs...)
}
