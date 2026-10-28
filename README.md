# file-replicator
A lightweight utility for versioning and replicating files between storage devices

## Architecture
This system relies on one or more configured Replication Groups, an independent set of directories that maintain the same relative structure, allowing you to version and replicate different subsets of file systems with different policies.

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
The config file includes the following fields (all must be present for the file to be valid):
- FileIndex: the path to the CSV file that has the index of all files, versions, and hashes
- Directories: an array of directories to include in scope for replication