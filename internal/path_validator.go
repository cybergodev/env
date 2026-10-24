// Provides path validation utilities for secure file access.

package internal

import (
	"path/filepath"
	"runtime"
	"strings"
)

// PathValidatorConfig holds configuration for path validation.
type PathValidatorConfig struct {
	// MaskKey is an optional function to mask sensitive key names in errors.
	// If nil, a default masking function is used.
	MaskKey func(string) string
}

// PathValidator validates file paths for security.
// It checks for path traversal attempts, absolute paths, and other dangerous patterns.
type PathValidator struct {
	maskKey func(string) string
}

// NewPathValidator creates a new PathValidator with the specified configuration.
func NewPathValidator(cfg PathValidatorConfig) *PathValidator {
	maskKey := cfg.MaskKey
	if maskKey == nil {
		maskKey = DefaultMaskKey
	}
	return &PathValidator{maskKey: maskKey}
}

// Validate validates a file path for security.
// It applies every rule of ValidateStatic and then resolves symlinks against
// the real file system to prevent symlink escape attacks (see validateSymlinks).
//
// Security checks performed:
//   - Empty filename
//   - Null bytes (could bypass extension checks)
//   - URL encoding (could bypass path checks)
//   - UNC paths (Windows network paths)
//   - Unix absolute paths
//   - Windows drive letters
//   - Path traversal (..)
//   - Windows reserved device names
//   - Symlink escape attacks
func (v *PathValidator) Validate(filename string) error {
	if err := v.ValidateStatic(filename); err != nil {
		return err
	}
	// SECURITY: Resolve and validate symlinks to prevent symlink escape attacks
	// A symlink within the allowed directory could point outside the intended scope
	return v.validateSymlinks(filepath.Clean(filename))
}

// ValidateStatic applies every disk-free path rule of Validate: the empty,
// null-byte, URL-encoding, control-character, UNC/network, rooted-path,
// drive-letter, traversal, reserved-device-name and (on Windows) alternate
// data stream checks. It performs NO syscalls — in particular no
// filepath.EvalSymlinks — which makes it the validation used for injected
// (non-OS) file systems: resolving symlinks there would consult the host
// disk that the injected file system never reads, making validation outcomes
// depend on files outside the injected boundary. The injected file system's
// own Open is the final arbiter of what exists.
func (v *PathValidator) ValidateStatic(filename string) error {
	if filename == "" {
		return &SecurityError{
			Action: "file_access",
			Reason: "empty filename",
		}
	}

	// SECURITY: Check for null bytes first (could be used to bypass extension checks)
	if strings.ContainsRune(filename, '\x00') {
		return &SecurityError{
			Action: "file_access",
			Reason: "null byte in path",
		}
	}

	// SECURITY: Reject URL encoding which could be used to bypass path checks
	// Examples: %2e%2e for .., %5c for \, etc.
	if strings.Contains(filename, "%") {
		return &SecurityError{
			Action: "file_access",
			Reason: "URL encoded path not allowed",
		}
	}

	// SECURITY (SEC-007): reject control characters other than tab in the
	// path. A newline (or other control byte) in a filename reaches the audit
	// log verbatim and lets a crafted name inject forged log records
	// (CWE-117). Tab is allowed (a legal filename character); everything else
	// below 0x20 is rejected here so no caller can route one into a log line.
	for i := 0; i < len(filename); i++ {
		c := filename[i]
		if c < 0x20 && c != '\t' {
			return &SecurityError{
				Action: "file_access",
				Reason: "control character in path not allowed",
			}
		}
	}

	// SECURITY: Check for UNC paths (Windows network paths)
	// These can be used to access files on network shares
	// Also block \\?\ prefix which can bypass path length limits
	if len(filename) >= 2 && filename[0] == '\\' && filename[1] == '\\' {
		return &SecurityError{
			Action: "file_access",
			Reason: "UNC path not allowed",
		}
	}

	// SECURITY: Check for forward-slash UNC paths (\\ translated to //)
	if len(filename) >= 2 && filename[0] == '/' && filename[1] == '/' {
		return &SecurityError{
			Action: "file_access",
			Reason: "network path not allowed",
		}
	}

	// SECURITY: Reject any path that starts with a path separator. '/' is
	// the Unix absolute form; a leading '\' is a drive-rooted path on
	// Windows ("\Windows\win.ini" resolves against the root of the current
	// drive even though filepath.IsAbs reports false for it — the path has
	// no volume name, and EvalSymlinks preserves the form). The check also
	// covers the root-local device spellings "\??\..." and "\.\...".
	// Rejected on every platform: '\'-leading names are exotic on Unix and
	// suspect in configuration paths regardless. Only allow relative paths
	// for safety.
	if len(filename) > 0 && (filename[0] == '/' || filename[0] == '\\') {
		return &SecurityError{
			Action: "file_access",
			Reason: "rooted path not allowed",
		}
	}

	// SECURITY: Check for Windows drive letters (C:, D:, etc.)
	// This prevents access to system files via absolute paths.
	// Drive letters are a Windows concept: on Unix "c:config" is a legal
	// relative filename and must not be rejected.
	if runtime.GOOS == "windows" && len(filename) >= 2 && filename[1] == ':' {
		c := filename[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return &SecurityError{
				Action: "file_access",
				Reason: "absolute path with drive letter not allowed",
			}
		}
	}

	// Check for path traversal attempts using filepath.Clean
	// filepath.Clean normalizes the path by resolving interior ".." and
	// removing redundant separators, so any ".." component that survives is
	// a genuine traversal. The comparison is per component, not a substring
	// test: "my..file.env" and "a..b/c.env" are legitimate names that a
	// Contains("..") check rejected (Clean keeps interior dots).
	cleanPath := filepath.Clean(filename)
	if hasDotDotComponent(cleanPath) {
		return &SecurityError{
			Action: "file_access",
			Reason: "path traversal detected",
			Key:    v.maskKey(filename),
		}
	}

	// SECURITY: On Windows, check for reserved device names
	// These names are reserved in any directory and cannot be used as filenames
	// See: https://learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file
	if err := v.checkReservedDeviceNames(filename); err != nil {
		return err
	}

	// SECURITY: On Windows a colon inside a path component denotes an NTFS
	// alternate data stream ("config/app.env:hidden") — a different file
	// identity than the one validated, and one whose trailing extension
	// (".env:hidden") misleads DetectFormat into the wrong parser. Drive
	// prefixes were already rejected above, so any remaining colon is
	// stream syntax; ':' remains a legal filename character on Unix.
	if runtime.GOOS == "windows" && hasComponentColon(cleanPath) {
		return &SecurityError{
			Action: "file_access",
			Reason: "alternate data stream path not allowed",
		}
	}

	return nil
}

// reservedDeviceNames and reservedPseudoDevices are the Windows reserved
// device names checked on every file access. Package-level so the check is
// allocation-free — per-call slice literals would rebuild their headers each
// time, and the previous strings.ToUpper copied the whole filename.
var (
	reservedDeviceNames   = []string{"CON", "PRN", "AUX", "NUL"}
	reservedPseudoDevices = []string{"CONIN$", "CONOUT$", "CLOCK$"}
)

// checkReservedDeviceNames checks every path component for Windows reserved
// device names. Reserved names apply in ANY directory, so checking only the
// leading component let later components through ("subdir/CON",
// "backup/NUL.txt"); the per-component walk closes that gap while leaving
// ordinary names that merely start with a reserved prefix ("config") valid.
// Comparisons are ASCII case-insensitive and allocation-free (EqualFoldASCII
// on component slices); ToUpper never maps a non-ASCII byte to one of these
// letters, so a fold mismatch here implies a mismatch there too.
func (v *PathValidator) checkReservedDeviceNames(filename string) error {
	for len(filename) > 0 {
		comp := filename
		if idx := indexByte2(filename, '/', '\\'); idx >= 0 {
			comp = filename[:idx]
			filename = filename[idx+1:]
		} else {
			filename = ""
		}
		if len(comp) >= 3 {
			if err := checkReservedComponent(comp); err != nil {
				return err
			}
		}
	}
	return nil
}

// indexByte2 returns the index of the first occurrence of a or b in s, or -1.
func indexByte2(s string, a, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == a || s[i] == b {
			return i
		}
	}
	return -1
}

// checkReservedComponent reports whether comp, a single path component, is a
// Windows reserved device name: CON/PRN/AUX/NUL (with or without an
// extension), COM1-COM9 / LPT1-LPT9, or the pseudo devices CONIN$/CONOUT$/
// CLOCK$. The caller guarantees len(comp) >= 3.
func checkReservedComponent(comp string) error {
	// Windows strips trailing spaces and dots from each path component
	// before resolving names, so "CON ", "CON." and "CON .txt" all address
	// the CON device. Reduce the component to its base name — everything
	// before the first '.' or ':', with trailing spaces and dots trimmed —
	// and match the reserved lists against that base.
	base := comp
	if i := strings.IndexAny(comp, ".:"); i >= 0 {
		base = comp[:i]
	}
	base = strings.TrimRight(base, " .")

	if len(base) >= 3 {
		// CON, PRN, AUX, NUL — exactly the base name ("CONfig" keeps its
		// longer base and stays an ordinary filename).
		for _, r := range reservedDeviceNames {
			if EqualFoldASCII(base, r) {
				return &SecurityError{
					Action: "file_access",
					Reason: "reserved device name",
				}
			}
		}

		// COM and LPT ports (COM1-COM9, LPT1-LPT9): 4-character bases.
		// Digits are case-invariant, so base[3] compares directly.
		if len(base) == 4 &&
			(EqualFoldASCII(base[:3], "COM") || EqualFoldASCII(base[:3], "LPT")) {
			if c := base[3]; c >= '1' && c <= '9' {
				return &SecurityError{
					Action: "file_access",
					Reason: "reserved device name",
				}
			}
		}
	}

	// Pseudo-device names: CONIN$, CONOUT$, CLOCK$ — the base reduction
	// above already covers "CONIN$", "CONIN$.txt" and "CONIN$ :x".
	for _, pd := range reservedPseudoDevices {
		if EqualFoldASCII(base, pd) {
			return &SecurityError{
				Action: "file_access",
				Reason: "reserved pseudo-device name",
			}
		}
	}

	return nil
}

// validateSymlinks resolves and validates symlinks to prevent escape attacks.
func (v *PathValidator) validateSymlinks(cleanPath string) error {
	resolved, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		// If the path doesn't exist yet (e.g., for write operations),
		// validate the parent directory instead
		dir := filepath.Dir(cleanPath)
		if dir != "." && dir != cleanPath {
			resolvedDir, dirErr := filepath.EvalSymlinks(dir)
			if dirErr != nil {
				// Path or parent doesn't exist - allow to proceed
				// The file operation will fail later if path is invalid
				return nil
			}
			// Validate resolved parent directory
			return v.validateResolvedPath(resolvedDir)
		}
		return nil
	}

	// Validate the resolved path is still within allowed bounds
	return v.validateResolvedPath(resolved)
}

// validateResolvedPath checks that a resolved (symlink-expanded) path is safe.
// It ensures the path is relative and doesn't escape to absolute locations.
func (v *PathValidator) validateResolvedPath(resolved string) error {
	// Check for absolute paths after symlink resolution
	if filepath.IsAbs(resolved) {
		return &SecurityError{
			Action: "file_access",
			Reason: "symlink resolves to absolute path",
		}
	}

	// Double-check for path traversal in resolved path (per component —
	// see hasDotDotComponent for why a substring test over-blocks)
	cleanResolved := filepath.Clean(resolved)
	if hasDotDotComponent(cleanResolved) {
		return &SecurityError{
			Action: "file_access",
			Reason: "symlink escapes allowed directory",
		}
	}

	return nil
}

// hasComponentColon reports whether any single path component contains a
// colon (components split on both separators, mirroring the
// checkReservedDeviceNames walk).
func hasComponentColon(path string) bool {
	for len(path) > 0 {
		comp := path
		if idx := indexByte2(path, '/', '\\'); idx >= 0 {
			comp = path[:idx]
			path = path[idx+1:]
		} else {
			path = ""
		}
		if strings.IndexByte(comp, ':') >= 0 {
			return true
		}
	}
	return false
}

// hasDotDotComponent reports whether any single component of path is exactly
// ".." (components split on both separators, mirroring the
// checkReservedDeviceNames walk). After filepath.Clean, surviving ".."
// components are the leading-parent forms ("../x"); interior parents have
// already been resolved away, and interior DOTS inside a name ("a..b") are
// not components and never were traversal.
func hasDotDotComponent(path string) bool {
	for len(path) > 0 {
		comp := path
		if idx := indexByte2(path, '/', '\\'); idx >= 0 {
			comp = path[:idx]
			path = path[idx+1:]
		} else {
			path = ""
		}
		if comp == ".." {
			return true
		}
	}
	return false
}

// DefaultPathValidator is the default path validator instance.
// It uses the default key masking function.
//
// It is a process-global knob rather than a Config setting: ValidateFilePath
// reads it without locking, so assign it at program init, before concurrent
// use of this package — mutating it concurrently with Load is a data race
// (the same contract DefaultFileSystem documents).
var DefaultPathValidator = NewPathValidator(PathValidatorConfig{})

// ValidateFilePath validates a file path using the default validator.
// This is a convenience function for backward compatibility.
func ValidateFilePath(filename string) error {
	return DefaultPathValidator.Validate(filename)
}

// ValidateFilePathStatic validates a file path using the default validator's
// static (disk-free) rules only — every rule of ValidateFilePath except the
// symlink resolution, which reads the real file system. See
// PathValidator.ValidateStatic for when this is the right fidelity.
func ValidateFilePathStatic(filename string) error {
	return DefaultPathValidator.ValidateStatic(filename)
}
