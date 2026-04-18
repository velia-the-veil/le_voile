//go:build windows

package config

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// applyRestrictedPerms sets a protected DACL on the file granting access only
// to LocalSystem (SYSTEM) and the local Administrators group. Inheritance is
// blocked so a permissive %AppData% parent ACL cannot widen the file's
// effective permissions.
//
// The pattern mirrors internal/ctlauth/perms_windows.go which solved the same
// threat model for ctl.token. Config TOML, integrity key, and HMAC file all
// sit under the service profile (`%AppData%\LeVoile\` when the service runs
// as LocalSystem) and must never be readable by unprivileged user processes
// — the UI talks to the service via IPC, not directly through the filesystem.
func applyRestrictedPerms(path string) error {
	systemSID, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return fmt.Errorf("config: create LocalSystem sid: %w", err)
	}
	adminsSID, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil {
		return fmt.Errorf("config: create Administrators sid: %w", err)
	}

	access := []windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(systemSID),
			},
		},
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(adminsSID),
			},
		},
	}

	dacl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return fmt.Errorf("config: acl from entries: %w", err)
	}

	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil,
	); err != nil {
		return fmt.Errorf("config: set named security info: %w", err)
	}
	return nil
}
