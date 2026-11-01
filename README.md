# file-replicator
A lightweight utility for versioning and replicating files between storage devices

Note: this application is in active development, comes with no guarantees (express or implied), and should not be considered fit for critical workloads

## Architecture
This system relies on one or more configured Replication Groups (RGs), an independent set of directories that maintain the same relative structure, allowing you to version and replicate different subsets of file systems with different policies. File operations (CRUD) are coordinated to prevent collisions during processing.

These file types are supported for advanced features (such as file diff):
- .json
- .md
- .txt
- .yaml

## Instructions
Build the executable (Windows example below):
```
    go build -o file-replicator.exe .
```

## Features
- [ ] Monitor for changes in files (using hash)
- [ ] Replicate files between devices (internal hard drives, external hard drives, flash drives, cloud filesystems, etc)
- [ ] Save and index previous versions of files
- [ ] For supported formats, provide a file diff between versions (start with .txt)
- [ ] Monitor disk space and health

## Config File Spec
The config file includes the following fields (all must be present for the file to be valid; reference the committed *config.json* for the structure):
- **Metadata / Description**: this description field is ignored by file-replicator and can be used to write context for users
- **Debug**: whether or not to generate debugging output (log files and console output)
- **ReplicationGroups**: one or more Replication Groups (RGs), an independent set of directories that maintain the same relative structure
- **ReplicationGroups / Name**: a friendly name of the Replication Group (must be unique among the configured RGs)
- **ReplicationGroups / FileIndex**: path to the CSV file that hold the file index for this Replication Group (must be unique among the configured RGs)
- **ReplicationGroups / ConflictAction**: when a version conflict occurs, this setting determines how to resolve it; valid options are *replicateNewest* (the newest version of the file is automatically replicated to the other directories), *never* (the application is read-only), and *ask* (user is given the option per-conflict)
- **ReplicationGroups / DisplayFileDiff**: when a version conflict occurs, this settings determins if/how to show the differences between the files; valid options are *automatic* (the diff is displayed by default), *never* (the diff feature is not available), and *ask* (user is given the option per-conflict)
- **ReplicationGroups / Directories**: an array of directories to include in scope for replication; if only one Replication Group with only one Directory is specified, the application will index, and optionally version, the files in that Directory