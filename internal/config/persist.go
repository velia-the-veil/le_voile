package config

// SaveAndSign persists the configuration atomically and re-signs the HMAC
// sidecar in the same critical section. Every writer in the project
// (IPC handlers, kill-switch persister, country selector, etc.) MUST go
// through SaveAndSign rather than calling cfg.Save directly — otherwise
// the Verify check at next boot would trip ErrIntegrityMismatch on an
// otherwise-legitimate write.
//
// The caller MUST hold config.Mu. See the package header on Mu for the
// rationale behind the serialized-writer contract.
//
// When key is nil or empty (legacy bootstrap paths, tests with integrity
// disabled), the Sign step is skipped so SaveAndSign behaves as Save.
// Production call-sites always pass a non-nil key wired from main.
//
// The HMAC is computed over the exact bytes that were just written
// (returned by saveBytes) rather than a re-read of the on-disk file.
// This closes a TOCTOU window where an attacker at service-level
// privileges could interpose between Save's rename and a disk re-read
// (NFR9j defense in depth; the primary protection is 0600/DACL perms).
func (c *Config) SaveAndSign(path string, key []byte) error {
	encoded, err := c.saveBytes(path)
	if err != nil {
		return err
	}
	if len(key) == 0 {
		return nil
	}
	return SignBytes(path, encoded, key)
}
