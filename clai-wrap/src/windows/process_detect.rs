//! Windows process detection module for clai-wrap.
//!
//! This module provides functionality to detect the foreground process running
//! in a ConPTY session. This is used for the privacy gate (Section 7.1 of the spec)
//! to determine whether output capture should be paused for sensitive applications
//! like `ssh`, `vim`, `sudo`, etc.
//!
//! # Platform Support
//!
//! This module is only available on Windows and uses the Tool Help Library to
//! walk the process tree.
//!
//! # Implementation Details
//!
//! ConPTY creates a "headless" console, making foreground process detection
//! non-trivial. We use the following approach:
//!
//! 1. Get the shell process ID (child of clai-wrap)
//! 2. Use `CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0)` to snapshot all processes
//! 3. Walk the process tree using `Process32First`/`Process32Next`
//! 4. Find leaf process(es) descended from shell PID
//! 5. Use `QueryFullProcessImageNameW()` on the leaf process handle
//!
//! The "foreground" process in a ConPTY context is typically the most recently
//! spawned descendant of the shell.
//!
//! # Failure Handling
//!
//! Process detection may fail due to permissions, race conditions, or platform quirks.
//! All failures are handled gracefully with appropriate fallbacks:
//!
//! | Failure | Handling |
//! |---------|----------|
//! | Process walk fails | Assume shell is foreground process |
//! | Process name is empty | Use "unknown" |

#![cfg(windows)]

use std::collections::{HashMap, HashSet};
use std::ffi::OsString;
use std::os::windows::ffi::OsStringExt;

use thiserror::Error;
use windows_sys::Win32::Foundation::{
    CloseHandle, GetLastError, ERROR_INSUFFICIENT_BUFFER, ERROR_NO_MORE_FILES, FALSE, HANDLE,
    INVALID_HANDLE_VALUE, MAX_PATH,
};
use windows_sys::Win32::System::Diagnostics::ToolHelp::{
    CreateToolhelp32Snapshot, Process32FirstW, Process32NextW, PROCESSENTRY32W, TH32CS_SNAPPROCESS,
};
use windows_sys::Win32::System::ProcessStatus::K32GetProcessImageFileNameW;
use windows_sys::Win32::System::Threading::{
    OpenProcess, QueryFullProcessImageNameW, PROCESS_NAME_WIN32, PROCESS_QUERY_INFORMATION,
    PROCESS_QUERY_LIMITED_INFORMATION,
};

/// Errors that can occur during Windows process detection.
#[derive(Debug, Error)]
pub enum ProcessDetectError {
    /// Failed to create process snapshot.
    #[error("failed to create process snapshot: error code {0}")]
    SnapshotFailed(u32),

    /// Failed to iterate processes.
    #[error("failed to iterate processes: error code {0}")]
    ProcessIterationFailed(u32),

    /// Failed to open process handle.
    #[error("failed to open process {pid}: error code {error_code}")]
    OpenProcessFailed { pid: u32, error_code: u32 },

    /// Failed to query process name.
    #[error("failed to query process name for pid {pid}: error code {error_code}")]
    QueryNameFailed { pid: u32, error_code: u32 },

    /// The process name was empty.
    #[error("process name is empty for pid {0}")]
    EmptyProcessName(u32),

    /// No descendant processes found.
    #[error("no descendant processes found for shell pid {0}")]
    NoDescendantsFound(u32),
}

/// Result type for Windows process detection operations.
pub type Result<T> = std::result::Result<T, ProcessDetectError>;

/// A process entry from the snapshot.
#[derive(Debug, Clone)]
struct ProcessInfo {
    /// Process ID.
    pid: u32,
    /// Parent process ID.
    parent_pid: u32,
    /// Executable name (from PROCESSENTRY32W.szExeFile).
    exe_name: String,
}

/// RAII wrapper for Windows HANDLE that closes on drop.
struct SafeHandle(HANDLE);

impl SafeHandle {
    /// Creates a new SafeHandle, returning None if the handle is invalid.
    fn new(handle: HANDLE) -> Option<Self> {
        if handle == INVALID_HANDLE_VALUE || handle == 0 as HANDLE {
            None
        } else {
            Some(Self(handle))
        }
    }

    /// Returns the raw handle value.
    fn as_raw(&self) -> HANDLE {
        self.0
    }
}

impl Drop for SafeHandle {
    fn drop(&mut self) {
        // SAFETY: We only create SafeHandle with valid handles,
        // and CloseHandle is safe to call on valid handles.
        unsafe {
            CloseHandle(self.0);
        }
    }
}

/// Create a snapshot of all running processes.
///
/// # Returns
///
/// A map of process ID to process info for all processes on the system.
///
/// # Errors
///
/// Returns an error if `CreateToolhelp32Snapshot` fails.
fn create_process_snapshot() -> Result<HashMap<u32, ProcessInfo>> {
    // SAFETY: CreateToolhelp32Snapshot is safe to call with these parameters.
    // TH32CS_SNAPPROCESS snapshots all processes on the system.
    let snapshot = unsafe { CreateToolhelp32Snapshot(TH32CS_SNAPPROCESS, 0) };

    let snapshot = SafeHandle::new(snapshot).ok_or_else(|| {
        // SAFETY: GetLastError is always safe to call.
        ProcessDetectError::SnapshotFailed(unsafe { GetLastError() })
    })?;

    let mut processes = HashMap::new();

    // Initialize PROCESSENTRY32W structure
    // SAFETY: PROCESSENTRY32W is a plain data structure with no pointers that need
    // special initialization. Zero-initialization is safe.
    let mut entry: PROCESSENTRY32W = unsafe { std::mem::zeroed() };
    entry.dwSize = std::mem::size_of::<PROCESSENTRY32W>() as u32;

    // Get the first process
    // SAFETY: Process32FirstW is safe to call with a valid snapshot handle
    // and a properly sized PROCESSENTRY32W structure.
    let success = unsafe { Process32FirstW(snapshot.as_raw(), &mut entry) };

    if success == FALSE {
        let error = unsafe { GetLastError() };
        if error != ERROR_NO_MORE_FILES {
            return Err(ProcessDetectError::ProcessIterationFailed(error));
        }
        // No processes found (very unlikely), return empty map
        return Ok(processes);
    }

    // Process the first entry
    add_process_entry(&mut processes, &entry);

    // Iterate through remaining processes
    loop {
        // SAFETY: Process32NextW is safe to call with a valid snapshot handle
        // and a properly sized PROCESSENTRY32W structure.
        let success = unsafe { Process32NextW(snapshot.as_raw(), &mut entry) };

        if success == FALSE {
            let error = unsafe { GetLastError() };
            if error == ERROR_NO_MORE_FILES {
                break; // Normal termination
            }
            return Err(ProcessDetectError::ProcessIterationFailed(error));
        }

        add_process_entry(&mut processes, &entry);
    }

    Ok(processes)
}

/// Add a process entry to the map.
fn add_process_entry(processes: &mut HashMap<u32, ProcessInfo>, entry: &PROCESSENTRY32W) {
    // Convert the exe name from wide string to String
    let exe_name = wide_to_string(&entry.szExeFile);

    processes.insert(
        entry.th32ProcessID,
        ProcessInfo {
            pid: entry.th32ProcessID,
            parent_pid: entry.th32ParentProcessID,
            exe_name,
        },
    );
}

/// Convert a null-terminated wide string to a Rust String.
fn wide_to_string(wide: &[u16]) -> String {
    // Find the null terminator
    let len = wide.iter().position(|&c| c == 0).unwrap_or(wide.len());
    let os_string = OsString::from_wide(&wide[..len]);
    os_string.to_string_lossy().into_owned()
}

/// Find all descendant processes of a given parent PID.
///
/// # Arguments
///
/// * `processes` - Map of all processes from snapshot.
/// * `root_pid` - The root process ID to find descendants of.
///
/// # Returns
///
/// A set of all descendant process IDs (not including the root itself).
fn find_descendants(processes: &HashMap<u32, ProcessInfo>, root_pid: u32) -> HashSet<u32> {
    let mut descendants = HashSet::new();
    let mut to_process = vec![root_pid];

    while let Some(current_pid) = to_process.pop() {
        // Find all direct children of current_pid
        for info in processes.values() {
            if info.parent_pid == current_pid && info.pid != root_pid {
                if descendants.insert(info.pid) {
                    // Only add to processing queue if we haven't seen this PID before
                    to_process.push(info.pid);
                }
            }
        }
    }

    descendants
}

/// Find the leaf process (most likely foreground) among descendants.
///
/// The "foreground" process in a ConPTY context is typically a leaf process
/// (one with no children) that is a descendant of the shell.
///
/// If multiple leaf processes exist, we return the one with the highest PID,
/// which is typically the most recently spawned.
///
/// # Arguments
///
/// * `processes` - Map of all processes from snapshot.
/// * `descendants` - Set of descendant PIDs to consider.
///
/// # Returns
///
/// The PID of the leaf process, or None if no descendants exist.
fn find_leaf_process(
    processes: &HashMap<u32, ProcessInfo>,
    descendants: &HashSet<u32>,
) -> Option<u32> {
    if descendants.is_empty() {
        return None;
    }

    // Build the set of PIDs that appear as a parent within the descendant set.
    // A process is a leaf if it is not in this parent set. O(N) vs the naive O(N²).
    let parent_pids: HashSet<u32> = descendants
        .iter()
        .filter_map(|&pid| processes.get(&pid).map(|info| info.parent_pid))
        .collect();
    let mut leaves: Vec<u32> = descendants
        .iter()
        .copied()
        .filter(|pid| !parent_pids.contains(pid))
        .collect();

    // Sort by PID (highest first) - most recently spawned is likely foreground
    leaves.sort_by(|a, b| b.cmp(a));

    leaves.first().copied()
}

/// Get the full process name (image path) for a given PID.
///
/// # Arguments
///
/// * `pid` - The process ID.
///
/// # Returns
///
/// The full path to the process executable.
///
/// # Errors
///
/// Returns an error if the process cannot be opened or the name cannot be queried.
pub fn get_process_image_name(pid: u32) -> Result<String> {
    // First try with PROCESS_QUERY_LIMITED_INFORMATION (works for more processes)
    // SAFETY: OpenProcess is safe to call with valid parameters.
    let handle = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid) };

    let handle = if let Some(h) = SafeHandle::new(handle) {
        h
    } else {
        // Try with PROCESS_QUERY_INFORMATION as fallback
        let handle = unsafe { OpenProcess(PROCESS_QUERY_INFORMATION, FALSE, pid) };
        SafeHandle::new(handle).ok_or_else(|| ProcessDetectError::OpenProcessFailed {
            pid,
            error_code: unsafe { GetLastError() },
        })?
    };

    // Buffer for the process name
    let mut buffer = [0u16; MAX_PATH as usize];
    let mut size = buffer.len() as u32;

    // SAFETY: QueryFullProcessImageNameW is safe to call with valid handle and buffer.
    let success = unsafe {
        QueryFullProcessImageNameW(
            handle.as_raw(),
            PROCESS_NAME_WIN32,
            buffer.as_mut_ptr(),
            &mut size,
        )
    };

    if success == FALSE {
        let error = unsafe { GetLastError() };

        // If buffer was too small, try with a larger buffer
        if error == ERROR_INSUFFICIENT_BUFFER {
            let mut large_buffer = vec![0u16; 32768];
            let mut large_size = large_buffer.len() as u32;

            let success = unsafe {
                QueryFullProcessImageNameW(
                    handle.as_raw(),
                    PROCESS_NAME_WIN32,
                    large_buffer.as_mut_ptr(),
                    &mut large_size,
                )
            };

            if success == FALSE {
                return Err(ProcessDetectError::QueryNameFailed {
                    pid,
                    error_code: unsafe { GetLastError() },
                });
            }

            let name = wide_to_string(&large_buffer[..large_size as usize]);
            if name.is_empty() {
                return Err(ProcessDetectError::EmptyProcessName(pid));
            }
            return Ok(name);
        }

        return Err(ProcessDetectError::QueryNameFailed {
            pid,
            error_code: error,
        });
    }

    let name = wide_to_string(&buffer[..size as usize]);
    if name.is_empty() {
        return Err(ProcessDetectError::EmptyProcessName(pid));
    }

    Ok(name)
}

/// Extract the executable name from a full path.
///
/// # Arguments
///
/// * `full_path` - The full path to the executable.
///
/// # Returns
///
/// Just the executable name without the path or extension.
pub fn extract_exe_name(full_path: &str) -> String {
    // Get the filename from the path
    let filename = full_path.rsplit(['\\', '/']).next().unwrap_or(full_path);

    // Remove .exe extension if present (case-insensitive)
    let lower = filename.to_lowercase();
    if lower.ends_with(".exe") {
        filename[..filename.len() - 4].to_string()
    } else {
        filename.to_string()
    }
}

/// Get the process name (just executable name, no path) for a given PID.
///
/// # Arguments
///
/// * `pid` - The process ID.
///
/// # Returns
///
/// The executable name of the process.
///
/// # Errors
///
/// Returns an error if the process name cannot be retrieved.
pub fn get_process_name(pid: u32) -> Result<String> {
    let full_path = get_process_image_name(pid)?;
    Ok(extract_exe_name(&full_path))
}

/// Get the foreground process name for a ConPTY session.
///
/// This is the main function to use for process detection on Windows.
/// It walks the process tree to find the leaf descendant of the shell,
/// which is typically the foreground process.
///
/// # Arguments
///
/// * `shell_pid` - The process ID of the shell (direct child of clai-wrap).
///
/// # Returns
///
/// The name of the foreground process.
///
/// # Errors
///
/// Returns an error if process detection fails.
pub fn get_foreground_process(shell_pid: u32) -> Result<String> {
    let processes = create_process_snapshot()?;

    // Find all descendants of the shell
    let descendants = find_descendants(&processes, shell_pid);

    // If no descendants, the shell itself is the foreground process
    if descendants.is_empty() {
        return get_process_name(shell_pid);
    }

    // Find the leaf process (most likely foreground)
    let leaf_pid = find_leaf_process(&processes, &descendants)
        .ok_or(ProcessDetectError::NoDescendantsFound(shell_pid))?;

    // Try to get the full process name
    match get_process_name(leaf_pid) {
        Ok(name) => Ok(name),
        Err(_) => {
            // Fall back to the exe name from the snapshot
            processes
                .get(&leaf_pid)
                .map(|info| extract_exe_name(&info.exe_name))
                .ok_or(ProcessDetectError::EmptyProcessName(leaf_pid))
        }
    }
}

/// Get the foreground process name with fallback.
///
/// This function attempts to get the foreground process name, but returns
/// a fallback value instead of an error if detection fails.
///
/// # Arguments
///
/// * `shell_pid` - The process ID of the shell.
/// * `fallback` - The fallback name to use if detection fails.
///
/// # Returns
///
/// The name of the foreground process, or the fallback if detection fails.
pub fn get_foreground_process_or(shell_pid: u32, fallback: &str) -> String {
    get_foreground_process(shell_pid).unwrap_or_else(|_| fallback.to_string())
}

/// Check if a process with the given PID exists.
///
/// This is useful for detecting stale/orphaned resources.
///
/// # Arguments
///
/// * `pid` - The process ID to check.
///
/// # Returns
///
/// `true` if the process exists, `false` otherwise.
pub fn process_exists(pid: u32) -> bool {
    // SAFETY: OpenProcess is safe to call with valid parameters.
    // If the process doesn't exist, it returns NULL/0.
    let handle = unsafe { OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, FALSE, pid) };

    if let Some(h) = SafeHandle::new(handle) {
        drop(h); // Close the handle
        true
    } else {
        false
    }
}

#[cfg(test)]
#[cfg(windows)]
mod tests {
    use super::*;

    #[test]
    fn test_create_process_snapshot() {
        // Should be able to create a snapshot
        let result = create_process_snapshot();
        assert!(
            result.is_ok(),
            "Failed to create snapshot: {:?}",
            result.err()
        );

        let processes = result.unwrap();
        // There should be at least some processes
        assert!(!processes.is_empty(), "No processes found in snapshot");

        // Our own process should be in the snapshot
        let own_pid = std::process::id();
        assert!(
            processes.contains_key(&own_pid),
            "Own process not found in snapshot"
        );
    }

    #[test]
    fn test_get_process_name_current_process() {
        let pid = std::process::id();
        let result = get_process_name(pid);

        assert!(
            result.is_ok(),
            "Failed to get process name for current process: {:?}",
            result.err()
        );

        let name = result.unwrap();
        assert!(!name.is_empty(), "Process name should not be empty");

        // The name should be reasonable length
        assert!(
            name.len() < 256,
            "Process name should be reasonably short: {}",
            name
        );

        eprintln!("Current process name: {}", name);
    }

    #[test]
    fn test_get_process_name_invalid_pid() {
        // Use an invalid PID
        let invalid_pid = 0xFFFFFFFF;
        let result = get_process_name(invalid_pid);

        assert!(result.is_err(), "Should fail for invalid PID");
    }

    #[test]
    fn test_extract_exe_name() {
        assert_eq!(extract_exe_name(r"C:\Windows\System32\cmd.exe"), "cmd");
        assert_eq!(
            extract_exe_name(r"C:\Program Files\PowerShell\7\pwsh.exe"),
            "pwsh"
        );
        assert_eq!(extract_exe_name("notepad.exe"), "notepad");
        assert_eq!(extract_exe_name("notepad"), "notepad");
        assert_eq!(extract_exe_name(r"/usr/bin/bash"), "bash");
    }

    #[test]
    fn test_wide_to_string() {
        // Test empty string
        let empty: &[u16] = &[0];
        assert_eq!(wide_to_string(empty), "");

        // Test simple ASCII
        let hello: Vec<u16> = "hello\0".encode_utf16().collect();
        assert_eq!(wide_to_string(&hello), "hello");

        // Test string without null terminator
        let no_null: Vec<u16> = "test".encode_utf16().collect();
        assert_eq!(wide_to_string(&no_null), "test");

        // Test with embedded null
        let with_null: Vec<u16> = "before\0after".encode_utf16().collect();
        assert_eq!(wide_to_string(&with_null), "before");
    }

    #[test]
    fn test_find_descendants() {
        let mut processes = HashMap::new();

        // Create a mock process tree:
        // PID 100 (root)
        //   ├── PID 200
        //   │   └── PID 300
        //   └── PID 201
        //       └── PID 301

        processes.insert(
            100,
            ProcessInfo {
                pid: 100,
                parent_pid: 1,
                exe_name: "root.exe".to_string(),
            },
        );
        processes.insert(
            200,
            ProcessInfo {
                pid: 200,
                parent_pid: 100,
                exe_name: "child1.exe".to_string(),
            },
        );
        processes.insert(
            201,
            ProcessInfo {
                pid: 201,
                parent_pid: 100,
                exe_name: "child2.exe".to_string(),
            },
        );
        processes.insert(
            300,
            ProcessInfo {
                pid: 300,
                parent_pid: 200,
                exe_name: "grandchild1.exe".to_string(),
            },
        );
        processes.insert(
            301,
            ProcessInfo {
                pid: 301,
                parent_pid: 201,
                exe_name: "grandchild2.exe".to_string(),
            },
        );

        let descendants = find_descendants(&processes, 100);

        assert_eq!(descendants.len(), 4);
        assert!(descendants.contains(&200));
        assert!(descendants.contains(&201));
        assert!(descendants.contains(&300));
        assert!(descendants.contains(&301));
        assert!(!descendants.contains(&100)); // Root should not be included
    }

    #[test]
    fn test_find_leaf_process() {
        let mut processes = HashMap::new();

        // Same tree as above
        processes.insert(
            100,
            ProcessInfo {
                pid: 100,
                parent_pid: 1,
                exe_name: "root.exe".to_string(),
            },
        );
        processes.insert(
            200,
            ProcessInfo {
                pid: 200,
                parent_pid: 100,
                exe_name: "child1.exe".to_string(),
            },
        );
        processes.insert(
            201,
            ProcessInfo {
                pid: 201,
                parent_pid: 100,
                exe_name: "child2.exe".to_string(),
            },
        );
        processes.insert(
            300,
            ProcessInfo {
                pid: 300,
                parent_pid: 200,
                exe_name: "grandchild1.exe".to_string(),
            },
        );
        processes.insert(
            301,
            ProcessInfo {
                pid: 301,
                parent_pid: 201,
                exe_name: "grandchild2.exe".to_string(),
            },
        );

        let descendants = find_descendants(&processes, 100);
        let leaf = find_leaf_process(&processes, &descendants);

        assert!(leaf.is_some());
        let leaf_pid = leaf.unwrap();
        // Should be 301 (highest PID among leaves)
        assert_eq!(leaf_pid, 301);
    }

    #[test]
    fn test_find_leaf_process_empty_descendants() {
        let processes = HashMap::new();
        let descendants = HashSet::new();

        let leaf = find_leaf_process(&processes, &descendants);
        assert!(leaf.is_none());
    }

    #[test]
    fn test_find_leaf_process_linear_chain() {
        // Chain: 1 -> 2 -> 3; leaf must be 3
        let mut processes = HashMap::new();
        for (pid, parent) in [(1u32, 0u32), (2, 1), (3, 2)] {
            processes.insert(
                pid,
                ProcessInfo {
                    pid,
                    parent_pid: parent,
                    exe_name: format!("{}.exe", pid),
                },
            );
        }
        let descendants: HashSet<u32> = [2, 3].into();
        let leaf = find_leaf_process(&processes, &descendants);
        assert_eq!(leaf, Some(3));
    }

    #[test]
    fn test_find_leaf_process_fork() {
        // Fork: 1 -> 2 and 1 -> 3; both 2 and 3 are leaves (highest PID wins)
        let mut processes = HashMap::new();
        for (pid, parent) in [(1u32, 0u32), (2, 1), (3, 1)] {
            processes.insert(
                pid,
                ProcessInfo {
                    pid,
                    parent_pid: parent,
                    exe_name: format!("{}.exe", pid),
                },
            );
        }
        let descendants: HashSet<u32> = [2, 3].into();
        let leaf = find_leaf_process(&processes, &descendants);
        // Both are leaves; sort_by highest PID first, so result is 3
        assert_eq!(leaf, Some(3));
    }

    #[test]
    fn test_find_leaf_process_single() {
        // Single descendant with no children — it is the leaf
        let mut processes = HashMap::new();
        processes.insert(
            42u32,
            ProcessInfo {
                pid: 42,
                parent_pid: 1,
                exe_name: "solo.exe".to_string(),
            },
        );
        let descendants: HashSet<u32> = [42].into();
        let leaf = find_leaf_process(&processes, &descendants);
        assert_eq!(leaf, Some(42));
    }

    #[test]
    fn test_process_exists() {
        // Our own process should exist
        let own_pid = std::process::id();
        assert!(process_exists(own_pid), "Own process should exist");

        // Invalid PID should not exist
        assert!(!process_exists(0xFFFFFFFF), "Invalid PID should not exist");
    }

    #[test]
    fn test_get_foreground_process_or_fallback() {
        // With an invalid PID, should return the fallback
        let fallback = "powershell";
        let name = get_foreground_process_or(0xFFFFFFFF, fallback);
        assert_eq!(name, fallback, "Should return fallback for invalid PID");
    }

    #[test]
    fn test_error_display() {
        let error = ProcessDetectError::SnapshotFailed(5);
        let display = error.to_string();
        assert!(
            display.contains("snapshot"),
            "Error display should mention snapshot: {}",
            display
        );

        let error = ProcessDetectError::OpenProcessFailed {
            pid: 1234,
            error_code: 5,
        };
        let display = error.to_string();
        assert!(
            display.contains("1234"),
            "Error display should contain pid: {}",
            display
        );

        let error = ProcessDetectError::EmptyProcessName(1234);
        let display = error.to_string();
        assert!(
            display.contains("empty"),
            "Error display should mention empty: {}",
            display
        );
    }

    /// Integration test that uses real Windows APIs.
    /// This test verifies we can walk the process tree from our own process.
    #[test]
    fn test_integration_process_tree_walk() {
        let own_pid = std::process::id();

        // Create snapshot
        let processes = create_process_snapshot().expect("Failed to create snapshot");

        // Find our own entry
        let own_entry = processes
            .get(&own_pid)
            .expect("Own process not in snapshot");
        eprintln!(
            "Own process: {} (PID {})",
            own_entry.exe_name, own_entry.pid
        );

        // Try to find our parent
        if let Some(parent_entry) = processes.get(&own_entry.parent_pid) {
            eprintln!(
                "Parent process: {} (PID {})",
                parent_entry.exe_name, parent_entry.pid
            );
        }

        // Find any descendants we might have
        let descendants = find_descendants(&processes, own_pid);
        if !descendants.is_empty() {
            eprintln!("Descendant PIDs: {:?}", descendants);
        } else {
            eprintln!("No descendants found (expected for test process)");
        }
    }

    /// Test that we can get the image name for system processes.
    /// Note: Some system processes may not be accessible due to permissions.
    #[test]
    fn test_get_process_image_name_system_process() {
        // Try to get the image name for a common system process
        // We'll iterate through processes and try to find one we can access
        let processes = create_process_snapshot().expect("Failed to create snapshot");

        let mut found_accessible = false;
        for (pid, info) in processes.iter().take(20) {
            match get_process_image_name(*pid) {
                Ok(path) => {
                    eprintln!("PID {}: {} -> {}", pid, info.exe_name, path);
                    found_accessible = true;
                }
                Err(e) => {
                    eprintln!("PID {}: {} -> Error: {}", pid, info.exe_name, e);
                }
            }
        }

        assert!(
            found_accessible,
            "Should be able to access at least one process"
        );
    }
}
