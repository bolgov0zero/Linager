//go:build windows

package agent

import (
	"golang.org/x/sys/windows"
)

// secureDir gives a directory to SYSTEM and the administrators only, and stops it from inheriting the access
// of its parent: the Windows counterpart of the 0700 and 0600 modes. Files created inside inherit this access.
func secureDir(dir string) error {
	// D: DACL, P: protected from inheritance, SY: SYSTEM, BA: built-in administrators, FA: full access, OICI: files and folders below
	sd, err := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, dacl, nil)
}
