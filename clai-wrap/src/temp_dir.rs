//! Temporary directory management for shell injection files.
//!
//! This module provides [`TempDirManager`] which creates and manages temporary
//! directories used for shell injection scripts. It handles:
//!
//! - Creating per-user base directories (`/tmp/clai-{uid}/`)
//! - Creating per-session directories (`session-{pid}/`)
//! - Shell-specific subdirectories for injection files
//! - Automatic cleanup on drop
//! - Stale session cleanup (orphaned directories from crashed processes)
//!
//! # Directory Structure
//!
//! ```text
//! /tmp/clai-{uid}/
//!   └── session-{pid}/
//!         ├── zsh/          # ZDOTDIR for zsh injection
//!         │   ├── .zshenv
//!         │   └── .zshrc
//!         ├── bash/         # rcfile for bash injection
//!         │   └── init.bash
//!         └── fish/         # init-command for fish
//! ```
//!
//! # Security
//!
//! - All directories are created with mode 0700 (user-only)
//! - UID is embedded in path to prevent cross-user access
//! - Path traversal is validated
//!
//! # Example
//!
//! ```no_run
//! use clai_wrap::temp_dir::TempDirManager;
//!
//! // Create a new session directory
//! let manager = TempDirManager::new().expect("failed to create temp dir");
//!
//! // Get or create a shell-specific subdirectory
//! let bash_dir = manager.shell_dir("bash").expect("failed to create bash dir");
//!
//! // The session directory is cleaned up when manager is dropped
//! ```

use std::fs::{self, File};
use std::io::{self, Write};
use std::path::{Path, PathBuf};
#[cfg(test)]
use std::sync::atomic::{AtomicU64, Ordering};

use thiserror::Error;
use tracing::{debug, warn};

/// Counter for generating unique session IDs in tests
#[cfg(test)]
static TEST_SESSION_COUNTER: AtomicU64 = AtomicU64::new(0);

/// Errors that can occur during temp directory management.
#[derive(Debug, Error)]
pub enum TempDirError {
    /// Failed to get the current user ID.
    #[error("failed to get user ID: {0}")]
    GetUserId(String),

    /// Failed to get the current process ID.
    #[error("failed to get process ID")]
    GetProcessId,

    /// Failed to create a directory.
    #[error("failed to create directory {path}: {source}")]
    CreateDir {
        path: PathBuf,
        #[source]
        source: io::Error,
    },

    /// Failed to set directory permissions.
    #[error("failed to set permissions on {path}: {source}")]
    SetPermissions {
        path: PathBuf,
        #[source]
        source: io::Error,
    },

    /// Failed to create or acquire lock file.
    #[error("failed to lock {path}: {source}")]
    Lock {
        path: PathBuf,
        #[source]
        source: io::Error,
    },

    /// Invalid shell name (contains path separator or is empty).
    #[error("invalid shell name: {0}")]
    InvalidShellName(String),

    /// Failed to read directory entries.
    #[error("failed to read directory {path}: {source}")]
    ReadDir {
        path: PathBuf,
        #[source]
        source: io::Error,
    },

    /// Failed to remove directory.
    #[error("failed to remove directory {path}: {source}")]
    RemoveDir {
        path: PathBuf,
        #[source]
        source: io::Error,
    },
}

/// Result type for temp directory operations.
pub type Result<T> = std::result::Result<T, TempDirError>;

/// Manages temporary directories for shell injection files.
///
/// Creates a per-session directory under a per-user base directory.
/// The session directory is automatically cleaned up when the manager is dropped.
///
/// # Directory Structure
///
/// - Base: `/tmp/clai-{uid}/` (or `$XDG_RUNTIME_DIR/clai/` if available)
/// - Session: `session-{pid}/`
/// - Shell subdirs: `zsh/`, `bash/`, `fish/`
///
/// # Cleanup
///
/// - Session directory is removed on drop
/// - Stale sessions (from crashed processes) can be cleaned via [`cleanup_stale`]
#[derive(Debug)]
pub struct TempDirManager {
    /// Base directory for all clai temp files (e.g., /tmp/clai-1000/)
    base_path: PathBuf,

    /// This session's directory (e.g., /tmp/clai-1000/session-12345/)
    session_dir: PathBuf,

    /// Path to the lock file for this session
    #[allow(dead_code)] // Stored for debugging/inspection purposes
    lock_file: PathBuf,

    /// The lock file handle (kept open to maintain the lock)
    #[allow(dead_code)] // Kept open to maintain the lock
    lock_handle: Option<File>,
}

impl TempDirManager {
    /// Creates a new temp directory manager.
    ///
    /// This creates:
    /// 1. A per-user base directory if it doesn't exist
    /// 2. A per-session directory for this process
    /// 3. A lock file to prevent cleanup races
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - The user ID cannot be determined
    /// - Directory creation fails
    /// - Permissions cannot be set
    /// - Lock file cannot be created
    pub fn new() -> Result<Self> {
        let uid = get_uid()?;
        let pid = std::process::id();

        // Determine base directory
        let base_path = get_base_dir(uid)?;

        Self::with_base_and_session_id(base_path, &format!("session-{pid}"))
    }

    /// Creates a temp directory manager with a custom base directory and session ID.
    ///
    /// This is primarily useful for testing where multiple managers may run
    /// in the same process.
    fn with_base_and_session_id(base_path: PathBuf, session_name: &str) -> Result<Self> {
        // Create base directory if needed (with secure permissions)
        create_dir_secure(&base_path)?;

        // Create session directory
        let session_dir = base_path.join(session_name);
        create_dir_secure(&session_dir)?;

        // Create and acquire lock file
        let lock_file = session_dir.join(".lock");
        let lock_handle = create_lock_file(&lock_file)?;

        debug!(
            base = %base_path.display(),
            session = %session_dir.display(),
            "created temp directory manager"
        );

        Ok(Self {
            base_path,
            session_dir,
            lock_file,
            lock_handle: Some(lock_handle),
        })
    }

    /// Creates a temp directory manager for testing with a unique session ID.
    ///
    /// Each call generates a unique session directory to avoid conflicts
    /// when tests run in parallel.
    #[cfg(test)]
    fn new_for_test() -> Result<Self> {
        let uid = get_uid()?;
        let base_path = get_base_dir(uid)?;
        let counter = TEST_SESSION_COUNTER.fetch_add(1, Ordering::SeqCst);
        let pid = std::process::id();
        let session_name = format!("test-{pid}-{counter}");

        Self::with_base_and_session_id(base_path, &session_name)
    }

    /// Returns the session directory path.
    ///
    /// This is the root directory for this session's temporary files.
    pub fn session_dir(&self) -> &Path {
        &self.session_dir
    }

    /// Returns the base directory path (shared across sessions for this user).
    pub fn base_dir(&self) -> &Path {
        &self.base_path
    }

    /// Gets or creates a subdirectory for a specific shell type.
    ///
    /// Valid shell names: `bash`, `zsh`, `fish`, `powershell`
    ///
    /// # Arguments
    ///
    /// * `shell` - The shell name (must not contain path separators)
    ///
    /// # Returns
    ///
    /// The path to the shell-specific subdirectory.
    ///
    /// # Errors
    ///
    /// Returns an error if:
    /// - The shell name is invalid (empty or contains path separators)
    /// - The directory cannot be created
    ///
    /// # Example
    ///
    /// ```no_run
    /// use clai_wrap::temp_dir::TempDirManager;
    ///
    /// let manager = TempDirManager::new().unwrap();
    /// let bash_dir = manager.shell_dir("bash").unwrap();
    /// // bash_dir is now /tmp/clai-{uid}/session-{pid}/bash/
    /// ```
    pub fn shell_dir(&self, shell: &str) -> Result<PathBuf> {
        // Validate shell name to prevent path traversal
        if shell.is_empty() {
            return Err(TempDirError::InvalidShellName(
                "shell name cannot be empty".to_string(),
            ));
        }

        if shell.contains('/') || shell.contains('\\') || shell.contains('\0') {
            return Err(TempDirError::InvalidShellName(format!(
                "shell name contains invalid characters: {shell}"
            )));
        }

        if shell == "." || shell == ".." {
            return Err(TempDirError::InvalidShellName(format!(
                "shell name cannot be '.' or '..': {shell}"
            )));
        }

        let shell_dir = self.session_dir.join(shell);
        create_dir_secure(&shell_dir)?;

        debug!(shell, path = %shell_dir.display(), "created shell directory");

        Ok(shell_dir)
    }

    /// Cleans up stale session directories from crashed processes.
    ///
    /// A session is considered stale if:
    /// 1. The PID embedded in the directory name no longer exists
    /// 2. The lock file cannot be exclusively locked (process is dead)
    ///
    /// This should be called at startup to clean up after crashes.
    ///
    /// # Returns
    ///
    /// The number of stale directories that were cleaned up.
    ///
    /// # Errors
    ///
    /// Returns an error if the base directory cannot be read.
    /// Individual cleanup failures are logged but do not stop the process.
    pub fn cleanup_stale() -> Result<usize> {
        let uid = get_uid()?;
        let base_path = get_base_dir(uid)?;

        if !base_path.exists() {
            return Ok(0);
        }

        let entries = fs::read_dir(&base_path).map_err(|e| TempDirError::ReadDir {
            path: base_path.clone(),
            source: e,
        })?;

        let mut cleaned = 0;

        for entry in entries.flatten() {
            let path = entry.path();

            // Only process session directories
            let Some(name) = path.file_name().and_then(|n| n.to_str()) else {
                continue;
            };

            if !name.starts_with("session-") {
                continue;
            }

            // Extract PID from directory name
            let Some(pid_str) = name.strip_prefix("session-") else {
                continue;
            };

            let Ok(pid) = pid_str.parse::<u32>() else {
                // Invalid pid format, skip
                warn!(path = %path.display(), "invalid session directory name");
                continue;
            };

            // Check if the process is still alive
            if is_process_alive(pid) {
                continue;
            }

            // Try to acquire exclusive lock on the lock file
            let lock_path = path.join(".lock");
            if lock_path.exists() && !try_acquire_exclusive_lock(&lock_path) {
                // Lock is held, process might still be alive
                debug!(
                    path = %path.display(),
                    pid,
                    "session directory locked, skipping"
                );
                continue;
            }

            // Process is dead and lock is free, clean up
            match fs::remove_dir_all(&path) {
                Ok(()) => {
                    debug!(path = %path.display(), pid, "cleaned up stale session");
                    cleaned += 1;
                }
                Err(e) => {
                    warn!(
                        path = %path.display(),
                        error = %e,
                        "failed to clean up stale session"
                    );
                }
            }
        }

        if cleaned > 0 {
            debug!(cleaned, "cleaned up stale session directories");
        }

        Ok(cleaned)
    }
}

impl Drop for TempDirManager {
    fn drop(&mut self) {
        // Release the lock by dropping the file handle
        self.lock_handle = None;

        // Remove the session directory
        if self.session_dir.exists() {
            if let Err(e) = fs::remove_dir_all(&self.session_dir) {
                warn!(
                    path = %self.session_dir.display(),
                    error = %e,
                    "failed to clean up session directory"
                );
            } else {
                debug!(path = %self.session_dir.display(), "cleaned up session directory");
            }
        }
    }
}

/// Gets the current user's UID.
#[cfg(unix)]
#[allow(clippy::unnecessary_wraps)] // Returns Result for API consistency with non-Unix
fn get_uid() -> Result<u32> {
    // SAFETY: getuid() is always safe to call and has no failure modes
    Ok(unsafe { libc::getuid() })
}

#[cfg(not(unix))]
fn get_uid() -> Result<u32> {
    // On Windows, use a hash of the username as a pseudo-UID
    std::env::var("USERNAME")
        .or_else(|_| std::env::var("USER"))
        .map(|name| {
            use std::collections::hash_map::DefaultHasher;
            use std::hash::{Hash, Hasher};
            let mut hasher = DefaultHasher::new();
            name.hash(&mut hasher);
            (hasher.finish() & 0xFFFF_FFFF) as u32
        })
        .map_err(|e| TempDirError::GetUserId(e.to_string()))
}

/// Gets the base directory for clai temp files.
#[allow(clippy::unnecessary_wraps)] // Returns Result for API consistency
fn get_base_dir(uid: u32) -> Result<PathBuf> {
    // Try XDG_RUNTIME_DIR first (usually /run/user/{uid}/)
    if let Ok(runtime_dir) = std::env::var("XDG_RUNTIME_DIR") {
        let path = PathBuf::from(runtime_dir).join("clai");
        return Ok(path);
    }

    // Fall back to /tmp/clai-{uid}/
    #[cfg(unix)]
    {
        Ok(PathBuf::from(format!("/tmp/clai-{uid}")))
    }

    #[cfg(not(unix))]
    {
        // On Windows, use %TEMP%\clai-{uid}
        let temp = std::env::var("TEMP")
            .or_else(|_| std::env::var("TMP"))
            .unwrap_or_else(|_| String::from("C:\\Windows\\Temp"));
        Ok(PathBuf::from(temp).join(format!("clai-{uid}")))
    }
}

/// Creates a directory with secure permissions (0700).
fn create_dir_secure(path: &Path) -> Result<()> {
    if path.exists() {
        // Verify it's a directory
        if !path.is_dir() {
            return Err(TempDirError::CreateDir {
                path: path.to_path_buf(),
                source: io::Error::new(
                    io::ErrorKind::AlreadyExists,
                    "path exists but is not a directory",
                ),
            });
        }
        return Ok(());
    }

    fs::create_dir_all(path).map_err(|e| TempDirError::CreateDir {
        path: path.to_path_buf(),
        source: e,
    })?;

    // Set permissions to 0700 on Unix
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        let permissions = fs::Permissions::from_mode(0o700);
        fs::set_permissions(path, permissions).map_err(|e| TempDirError::SetPermissions {
            path: path.to_path_buf(),
            source: e,
        })?;
    }

    Ok(())
}

/// Creates a lock file and returns a handle with an exclusive lock.
fn create_lock_file(path: &Path) -> Result<File> {
    let file = File::create(path).map_err(|e| TempDirError::Lock {
        path: path.to_path_buf(),
        source: e,
    })?;

    // Write PID to lock file for debugging
    let pid = std::process::id();
    let mut file = file;
    let _ = file.write_all(format!("{pid}\n").as_bytes());

    // Acquire exclusive lock
    #[cfg(unix)]
    {
        use std::os::unix::io::AsRawFd;
        let fd = file.as_raw_fd();

        // SAFETY: We're calling flock on a valid file descriptor
        let result = unsafe { libc::flock(fd, libc::LOCK_EX | libc::LOCK_NB) };
        if result != 0 {
            return Err(TempDirError::Lock {
                path: path.to_path_buf(),
                source: io::Error::last_os_error(),
            });
        }
    }

    #[cfg(windows)]
    {
        // Windows file locking would go here
        // For now, we rely on the file being open
    }

    Ok(file)
}

/// Checks if a process with the given PID is still alive.
#[cfg(unix)]
#[allow(clippy::cast_possible_wrap)] // PID values are always positive and within i32 range
fn is_process_alive(pid: u32) -> bool {
    // SAFETY: kill(pid, 0) is safe - it just checks if the process exists
    let result = unsafe { libc::kill(pid as libc::pid_t, 0) };
    if result == 0 {
        return true;
    }

    // Check the error code
    let err = io::Error::last_os_error();
    // ESRCH means "no such process"
    err.raw_os_error() != Some(libc::ESRCH)
}

#[cfg(not(unix))]
fn is_process_alive(pid: u32) -> bool {
    // On Windows, we would use OpenProcess
    // For now, assume alive if we can't check
    // This is conservative - we won't clean up directories that might be in use
    true
}

/// Tries to acquire an exclusive lock on a file.
/// Returns true if the lock was acquired (meaning no one else has it).
#[cfg(unix)]
fn try_acquire_exclusive_lock(path: &Path) -> bool {
    use std::os::unix::io::AsRawFd;

    let Ok(file) = File::open(path) else {
        return true; // Can't open, probably okay to clean up
    };

    let fd = file.as_raw_fd();

    // Try non-blocking exclusive lock
    // SAFETY: We're calling flock on a valid file descriptor
    let result = unsafe { libc::flock(fd, libc::LOCK_EX | libc::LOCK_NB) };
    if result == 0 {
        // We got the lock, which means no one else had it
        // Release it immediately
        unsafe { libc::flock(fd, libc::LOCK_UN) };
        return true;
    }

    // Couldn't get lock, someone else has it
    false
}

#[cfg(not(unix))]
fn try_acquire_exclusive_lock(_path: &Path) -> bool {
    // On Windows, assume we can't acquire the lock
    // This is conservative
    false
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_new_creates_directories() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        // Base directory should exist
        assert!(manager.base_dir().exists(), "base directory should exist");
        assert!(
            manager.base_dir().is_dir(),
            "base directory should be a directory"
        );

        // Session directory should exist
        assert!(
            manager.session_dir().exists(),
            "session directory should exist"
        );
        assert!(
            manager.session_dir().is_dir(),
            "session directory should be a directory"
        );

        // Session directory should be under base directory
        assert!(
            manager.session_dir().starts_with(manager.base_dir()),
            "session directory should be under base directory"
        );

        // Session directory name should start with test-
        let session_name = manager.session_dir().file_name().unwrap().to_str().unwrap();
        assert!(
            session_name.starts_with("test-"),
            "test session directory should start with 'test-', got: {}",
            session_name
        );
    }

    #[test]
    fn test_shell_dir_creates_subdirectory() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        let bash_dir = manager
            .shell_dir("bash")
            .expect("failed to create bash dir");

        assert!(bash_dir.exists(), "bash directory should exist");
        assert!(bash_dir.is_dir(), "bash directory should be a directory");
        assert_eq!(
            bash_dir.file_name().unwrap().to_str().unwrap(),
            "bash",
            "directory name should be 'bash'"
        );
        assert!(
            bash_dir.starts_with(manager.session_dir()),
            "shell directory should be under session directory"
        );
    }

    #[test]
    fn test_shell_dir_multiple_shells() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        let bash_dir = manager
            .shell_dir("bash")
            .expect("failed to create bash dir");
        let zsh_dir = manager.shell_dir("zsh").expect("failed to create zsh dir");
        let fish_dir = manager
            .shell_dir("fish")
            .expect("failed to create fish dir");

        assert!(bash_dir.exists());
        assert!(zsh_dir.exists());
        assert!(fish_dir.exists());

        // All should be different
        assert_ne!(bash_dir, zsh_dir);
        assert_ne!(bash_dir, fish_dir);
        assert_ne!(zsh_dir, fish_dir);
    }

    #[test]
    fn test_shell_dir_idempotent() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        let bash_dir1 = manager.shell_dir("bash").expect("first call failed");
        let bash_dir2 = manager.shell_dir("bash").expect("second call failed");

        assert_eq!(bash_dir1, bash_dir2, "shell_dir should be idempotent");
    }

    #[test]
    fn test_shell_dir_rejects_empty_name() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        let result = manager.shell_dir("");
        assert!(result.is_err(), "empty shell name should be rejected");

        let err = result.unwrap_err();
        assert!(
            matches!(err, TempDirError::InvalidShellName(_)),
            "should be InvalidShellName error"
        );
    }

    #[test]
    fn test_shell_dir_rejects_path_traversal() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        // Forward slash
        assert!(manager.shell_dir("../bash").is_err(), "should reject ../");
        assert!(manager.shell_dir("foo/bar").is_err(), "should reject /");

        // Backslash (Windows path separator)
        assert!(manager.shell_dir("..\\bash").is_err(), "should reject ..\\");
        assert!(manager.shell_dir("foo\\bar").is_err(), "should reject \\");

        // Null byte
        assert!(
            manager.shell_dir("bash\0evil").is_err(),
            "should reject null byte"
        );

        // Dot directories
        assert!(manager.shell_dir(".").is_err(), "should reject .");
        assert!(manager.shell_dir("..").is_err(), "should reject ..");
    }

    #[test]
    fn test_cleanup_on_drop() {
        let session_path;

        {
            let manager = TempDirManager::new_for_test().expect("failed to create manager");
            session_path = manager.session_dir().to_path_buf();

            // Create a shell subdirectory to ensure recursive cleanup
            let _ = manager
                .shell_dir("bash")
                .expect("failed to create bash dir");

            assert!(session_path.exists(), "session should exist before drop");
        }

        // After drop, session directory should be cleaned up
        assert!(
            !session_path.exists(),
            "session directory should be cleaned up on drop"
        );
    }

    #[test]
    fn test_lock_file_created() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        let lock_path = manager.session_dir().join(".lock");
        assert!(lock_path.exists(), "lock file should exist");

        // Lock file should contain our PID
        let content = fs::read_to_string(&lock_path).expect("failed to read lock file");
        let expected_pid = format!("{}\n", std::process::id());
        assert_eq!(content, expected_pid, "lock file should contain PID");
    }

    #[test]
    fn test_multiple_managers_independent() {
        let manager1 = TempDirManager::new_for_test().expect("failed to create manager 1");
        let manager2 = TempDirManager::new_for_test().expect("failed to create manager 2");

        // The base directory should be the same
        assert_eq!(manager1.base_dir(), manager2.base_dir());

        // But session directories should be different
        assert_ne!(
            manager1.session_dir(),
            manager2.session_dir(),
            "test managers should have different session directories"
        );

        // Both should exist
        assert!(manager1.session_dir().exists());
        assert!(manager2.session_dir().exists());
    }

    #[cfg(unix)]
    #[test]
    fn test_permissions_are_secure() {
        use std::os::unix::fs::PermissionsExt;

        let manager = TempDirManager::new_for_test().expect("failed to create manager");

        // Check base directory permissions
        let base_meta = fs::metadata(manager.base_dir()).expect("failed to get base metadata");
        let base_mode = base_meta.permissions().mode() & 0o777;
        assert_eq!(
            base_mode, 0o700,
            "base directory should have 0700 permissions, got {:o}",
            base_mode
        );

        // Check session directory permissions
        let session_meta =
            fs::metadata(manager.session_dir()).expect("failed to get session metadata");
        let session_mode = session_meta.permissions().mode() & 0o777;
        assert_eq!(
            session_mode, 0o700,
            "session directory should have 0700 permissions, got {:o}",
            session_mode
        );

        // Check shell subdirectory permissions
        let bash_dir = manager
            .shell_dir("bash")
            .expect("failed to create bash dir");
        let bash_meta = fs::metadata(&bash_dir).expect("failed to get bash metadata");
        let bash_mode = bash_meta.permissions().mode() & 0o777;
        assert_eq!(
            bash_mode, 0o700,
            "shell directory should have 0700 permissions, got {:o}",
            bash_mode
        );
    }

    #[test]
    fn test_cleanup_stale_does_not_error_on_missing_base() {
        // If the base directory doesn't exist, cleanup_stale should return Ok(0)
        // We can't easily test this without mocking, but we can at least verify
        // that cleanup_stale doesn't panic
        let result = TempDirManager::cleanup_stale();
        assert!(result.is_ok(), "cleanup_stale should not error");
    }

    #[cfg(unix)]
    #[test]
    fn test_is_process_alive_current_process() {
        let pid = std::process::id();
        assert!(is_process_alive(pid), "current process should be alive");
    }

    #[cfg(unix)]
    #[test]
    fn test_is_process_alive_nonexistent_process() {
        // Use a very high PID that's unlikely to exist
        // Note: PIDs can wrap around, so this isn't perfect
        let fake_pid = 999_999_999;
        assert!(
            !is_process_alive(fake_pid),
            "nonexistent process should not be alive"
        );
    }

    #[test]
    fn test_get_base_dir_contains_uid() {
        let uid = get_uid().expect("failed to get uid");
        let base = get_base_dir(uid).expect("failed to get base dir");

        // Either XDG_RUNTIME_DIR/clai or /tmp/clai-{uid}
        let path_str = base.to_string_lossy();
        let contains_uid = path_str.contains(&format!("-{uid}")) || path_str.contains("/clai");
        assert!(
            contains_uid,
            "base directory should contain uid or be under XDG_RUNTIME_DIR"
        );
    }

    // Test for stale cleanup - this is harder to test without spawning processes
    // We test the basic scenario where there are no stale directories
    #[test]
    fn test_cleanup_stale_with_active_session() {
        let manager = TempDirManager::new_for_test().expect("failed to create manager");
        let session_path = manager.session_dir().to_path_buf();

        // Run cleanup - our session should NOT be cleaned up because we're still alive
        // (test sessions use current PID in the name)
        let _cleaned = TempDirManager::cleanup_stale().expect("cleanup failed");

        // Our session should still exist (process is alive, lock is held)
        assert!(
            session_path.exists(),
            "active session should not be cleaned up"
        );

        drop(manager);

        // After dropping, the session should be gone (normal cleanup, not stale cleanup)
        assert!(
            !session_path.exists(),
            "session should be cleaned up on drop"
        );
    }

    #[test]
    fn test_stale_directory_cleanup() {
        // Create a directory that looks like a stale session (non-existent PID)
        let uid = get_uid().expect("failed to get uid");
        let base_path = get_base_dir(uid).expect("failed to get base dir");
        create_dir_secure(&base_path).expect("failed to create base dir");

        // Use a very high PID that definitely doesn't exist
        let fake_pid = 999_999_998;
        let stale_dir = base_path.join(format!("session-{fake_pid}"));

        // Only proceed if the fake PID doesn't exist
        if !is_process_alive(fake_pid) {
            fs::create_dir_all(&stale_dir).expect("failed to create stale dir");

            // Don't create a lock file - simulating a crash before lock was created
            assert!(stale_dir.exists(), "stale dir should exist before cleanup");

            // Run cleanup
            let cleaned = TempDirManager::cleanup_stale().expect("cleanup failed");

            // The stale directory should be cleaned up
            assert!(!stale_dir.exists(), "stale directory should be cleaned up");
            assert!(cleaned >= 1, "should have cleaned at least one directory");
        }
    }
}
