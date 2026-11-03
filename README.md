# file-replicator
A lightweight utility for versioning and replicating files among storage devices

Note: this application is in active development, comes with no guarantees (express or implied), and should not be considered fit for critical workloads

## Architecture
This system relies on one or more configured Replication Groups (RGs), an independent set of directories that maintain the same relative structure, allowing you to version and replicate different subsets of file systems with different policies. File operations (CRUD and hash) are coordinated to prevent collisions during processing.

The File Index holds an inventory of every file and all existing versions of that file in the Replication Group. The index represents the last-known state of all file versions, and running file-replicator will re-align the index with the current state of the configured directories. The latest version of the file is always stored with the file name set by the user; previous versions are stored in the *.versions* directory and named *{filename}_frv{version#}.{extension}* (for example, MyFile_frv2.txt). Once a file is versioned, it should not be edited further.

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
- [ ] Replicate files among devices (internal hard drives, external hard drives, flash drives, cloud filesystems, etc)
- [ ] Save and index previous versions of files
- [ ] For supported formats, provide a file diff for conflicts
- [ ] Monitor disk space and health

## Config File Spec
The config file includes the following fields (all must be present for the file to be valid; reference the committed *config.json* for the structure):
- **Metadata / Description**: this description field is ignored by file-replicator and can be used to write context for users
- **Debug**: whether or not to generate debugging output (log files and console output)
- **ReplicationGroups**: one or more Replication Groups (RGs), an independent set of directories that maintain the same relative structure
- **ReplicationGroups / Name**: a friendly name of the Replication Group (must be unique among the configured RGs)
- **ReplicationGroups / FileIndex**: path to the CSV file that hold the file index for this Replication Group (must be unique among the configured RGs)
- **ReplicationGroups / HashConflictAction**: when a hash conflict occurs among the files, this setting determines how to resolve it; valid options are *replicateNewest* (the newest version of the file is automatically replicated to the other directories), *never* (file hash conflicts are reported on but no action is taken), and *ask* (user is given the option per-conflict)
- **ReplicationGroups / VersionConflictAction**: when a version conflict occurs (the hash of the files does not match the latest version hash in the File Index), this setting determines how to resolve it; valid options are *updateFileIndex* (the hash of the file in the File Index is replaced for the latest version), *never* (version conflicts are reported on but no action is taken), and *ask* (user is given the option per-conflict)
- **ReplicationGroups / MissingFileAction**: when a file exists in one or more directories and is missing in one or more other directories, this setting determines how to resolve it; valid options are *createMissing* (the existing file is copied to the directories where it is missing), *deleteExisting* (the instances of the file that are still present are deleted), *never* (missing files are reported on but no action is taken), and *ask* (user is given the option per-file); hash conflicts are resolved before missing files, so it does not matter which file is the source for the copy
- **ReplicationGroups / DisplayFileDiff**: when a hash conflict occurs, this settings determins if/how to show the differences among the files; valid options are *automatic* (the diff is displayed by default), *never* (the diff feature is not available), and *ask* (user is given the option per-conflict)
- **ReplicationGroups / Directories**: an array of directories to include in scope for replication; if only one Replication Group with only one Directory is specified, the application will index, and optionally version, the files in that Directory