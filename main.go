package main

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// The Metadata fields from the config file
type Metadata struct {
	Description []string // a description of the configuration file that is ignored by the application
}

// The elements of a single Replication Group configuration
type ReplicationGroup struct {
	Name                  string   // name of the Replication Group
	FileIndex             string   // name of the CSV file that holds the file index
	HashConflictAction    string   // action to take for hash conflicts among files
	VersionConflictAction string   // action to take for conflicts between the file hash and its latest version hash in the File Index
	MissingFileAction     string   // action to take if a file exists in one or more directories and is missing in one or more other directories
	DisplayFileDiff       string   // settings for displaying the diff for supported file types
	Directories           []string // one or more directories to include in the Replication Group's context
}

// The structure of this struct must match the JSON structure of the config file
type Config struct {
	Metadata          Metadata
	Debug             bool // whether or not to generate debugging output
	ReplicationGroups []ReplicationGroup
}

// This type represents the request for and results of an operation performed on a specific file
type FileOperation struct {
	Operation string           // the file operation to complete - read, write, create, delete, or hash
	Filepath  string           // the path and name of the file
	Input     string           // any input relevant to the operation (such as content to write)
	Result    chan interface{} // the result of the file operation, such as the file contents for a read operation or the hash for a hash operation
	Error     chan error       // any error produced during the file operation; error value is nil if the operation was successful
}

// Helper function to check if an element is in a slice for any of the comparable types
func sliceContains[T comparable](s []T, e T) bool {
	for _, i := range s {
		if i == e {
			return true
		}
	}
	return false
}

// Helper function to check if all elements in a slice are equal for any of the comparable types
func sliceElementsEqual[T comparable](s []T) bool {
	for _, i1 := range s {
		for _, i2 := range s {
			if i1 != i2 {
				return false
			}
		}
	}
	return true
}

// Return the File Index CSV header
func getFileIndexHeader() string {
	return "filepath,hash,version"
}

// Parse the configuration file and validate its contents, returning the config as an object
func parseConfig(configFile string) (Config, error) {
	// Read the configuration file from disk
	data, err := os.ReadFile(configFile)
	if err != nil {
		return Config{}, err
	}

	// Parse the configuration file into the Config object type
	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return Config{}, fmt.Errorf("Failed to parse config: %w", err)
	}

	// Validate that there are no duplicate Replication Group names
	var replicationGroupNames []string
	for _, r := range config.ReplicationGroups {
		if sliceContains(replicationGroupNames, r.Name) {
			return Config{}, fmt.Errorf("Replication Group name '%s' in %s is not unique", r.Name, configFile)
		}
		replicationGroupNames = append(replicationGroupNames, r.Name)
	}

	// Validate that there are no duplicate Replication Group file indexes
	var replicationGroupFileIndexes []string
	for _, r := range config.ReplicationGroups {
		if sliceContains(replicationGroupFileIndexes, r.FileIndex) {
			return Config{}, fmt.Errorf("Replication Group file index '%s' in %s is not unique", r.FileIndex, configFile)
		}
		replicationGroupFileIndexes = append(replicationGroupFileIndexes, r.FileIndex)
	}

	return config, nil
}

// Given the path to the File Index, return a structured list of files and their metadata
func parseFileIndex(f string) ([][]string, error) {
	// Define the target header row of the File Index CSV
	var headerRow string = getFileIndexHeader()

	// Attempt to load the File Index for this Replication Group
	fileIndex, err := os.Open(f)
	if err != nil {
		// Create the index and open it if it is not present, otherwise fail
		var pathError *fs.PathError
		if errors.As(err, &pathError) {
			// Create the file index, using the declared fileIndex variable from the original open attempt
			fileIndex, err = os.Create(f)
			if err != nil {
				return [][]string{}, err
			}

			// Write the CSV header to the file
			n, err := fileIndex.Write([]byte(headerRow + "\n"))
			if err != nil || n == 0 {
				return [][]string{}, fmt.Errorf("Could not write fileIndex header in function parseFileIndex: %e", err)
			}

			// Reset the cursor position to the beginning of the file for the subsequent read
			fileIndex.Seek(0, io.SeekStart)
		} else {
			// Fail if the error from the attempted file open was not a PathError
			return [][]string{}, err
		}
	}

	// At this point, we have an open File Index with no read/write offset; defer the file close before proceeding
	defer fileIndex.Close()

	// Read the contents of the File Index into fileIndexContents and store as a string in fileIndexContentsString
	var fileIndexContents []byte = make([]byte, 1024) // read the file index in 1024-byte chunks
	var fileIndexContentsString string
	for {
		// Read the next chunk of data from the file
		n, err := fileIndex.Read(fileIndexContents)
		if err != nil && err == io.EOF {
			// This section is hit when there are no bytes to be read from the file
			// If there are <1024 bytes (the []byte buffer is not filled), then that content is still read without error
			break
		} else if err != nil && err != io.EOF {
			return [][]string{}, fmt.Errorf("Could not read fileIndex file: %e", err)
		} else {
			fileIndexContentsString += string(fileIndexContents[:n]) // only append the number of bytes read (n) to the string to avoid padding up to 1024 bytes
		}
	}

	// Trim any trailing newlines
	fileIndexContentsString = strings.Trim(fileIndexContentsString, "\n")

	// Validate the header row is present and matches expected content
	if strings.Split(fileIndexContentsString, "\n")[0] != headerRow {
		return [][]string{}, fmt.Errorf("File Index %s header row '%s' is not valid", f, strings.Split(fileIndexContentsString, "\n")[0])
	}

	// Iterate over each row in the CSV, storing the fields into the fileIndexStructured multidimensional array
	var fileIndexStructured [][]string
	for _, r := range strings.Split(fileIndexContentsString, "\n")[1:] { // strings.Split removes the newlines during the split, so no trim is needed
		// Split the row to get each element
		var e []string = strings.Split(r, ",")

		// Verify the number of elements matches the file structure (derived from the header)
		if len(e) != len(strings.Split(headerRow, ",")) {
			return [][]string{}, fmt.Errorf("File Index row '%s' is not valid", r)
		}

		// Append that slice of elements to the fileIndexStructured multidimensional array
		fileIndexStructured = append(fileIndexStructured, e)
	}

	return fileIndexStructured, nil
}

// Given the path to the File Index and the in-memory File Index contents, write the File Index to the CSV file
// The file is entirely rewritten as part of the Replication Group processing
func writeFileIndex(f string, fileIndex [][]string) error {
	// Define the target header row of the File Index CSV
	var headerRow string = getFileIndexHeader()

	// Attempt to load the File Index for this Replication Group
	file, err := os.OpenFile(f, os.O_RDWR, 0644)
	if err != nil {
		return err
	}

	// At this point, we have an open File Index; defer the file close before proceeding
	defer file.Close()

	// Write the CSV header to the file
	_, err = file.Write([]byte(headerRow + "\n"))
	if err != nil {
		return fmt.Errorf("Could not write fileIndex header in function writeFileIndex: %e", err)
	}

	// Iterate through the potentially modified File Index from memory, writing each entry to the CSV file
	for _, e := range fileIndex {
		n, err := file.Write([]byte(e[0] + "," + e[1] + "," + e[2] + "\n"))
		if err != nil || n == 0 {
			return fmt.Errorf("Could not write a line to File Index %s: %e", f, err)
		}
	}

	return nil
}

// Get the full filepaths for every file in the configured directories, appending them to the provided slice
// Called recursively to iterate over all sub-directories
// Fuction takes a pointer to the filepaths slice (declared in main()), the target directory (full path), and whether to recurse through subdirectories
func getFiles(filepaths *[]string, directory string, recursive bool) error {
	files, err := os.ReadDir(directory)
	if err != nil {
		return err
	}

	for _, f := range files {
		if recursive && f.IsDir() {
			err = getFiles(filepaths, filepath.Join(directory, f.Name()), recursive) // recursive call with the filepaths pointer
			if err != nil {
				return err
			}
		} else {
			*filepaths = append(*filepaths, filepath.Join(directory, f.Name()))
		}
	}

	return nil
}

// Return the hash of a given file
// Two files with different filenames and/or filepaths, but idential contents, will return the same hash
func getFileHash(filepath string) (string, error) {
	// Opens the named file for reading
	file, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Create a new hash object and then stream the file contents into the hash
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	// Return the file hash in hexadecimal format as a string
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// The fileController function gates file access for each processReplicationGroup goroutine to ensure goroutines do not collide trying to process the same file
// fileController is the only function that should be performing operations on scoped files/directories (excluding file-replicator configuration and index files)
func fileController(opsChannel chan FileOperation) {
	// fileController receives requests over the opsChannel (file operations channel)
	for op := range opsChannel { // range on a channel only produces one value - the element itself
		switch op.Operation {

		// Read the contents of the file, saving the contents or error in the channel
		case "read":
			data, err := os.ReadFile(op.Filepath)
			op.Result <- data
			op.Error <- err

		// Return the file hash
		case "hash":
			hash, err := getFileHash(op.Filepath)
			op.Result <- hash
			op.Error <- err // if successful, getFileHash returns a nil error, so fileController sets the error channel to nil

		// Error if an invalid file operation is specified
		default:
			op.Error <- fmt.Errorf("Unknown file operation '%s'", op.Operation)
		}
	}
}

// This is the root function that handles the processing for a specific Replication Group, and it is called as a goroutine.
// Each Replication Group is handled by exactly one processReplicationGroup goroutine, and each goroutine processes only one Replication Group.
// Each instance of this function is responsible for its own downstream processing (error handling, reporting, user interaction, etc).
// This function will complete these tasks, then return:
// 1) Iterate over the "filepaths" slice and evaluate each file in the first of the directories in the RG
// 2) Iterate through each of the other directories in the RG, handling any files present that were not present in the initial loop (Step 1)
// 3) Rewrite the File Index file with the changes to the fileIndex multidimensional array
// The fileController function coordinates access to files among the processReplicationGroup goroutines, and communication is handled by the opsChannel.
func processReplicationGroup(rgConfig ReplicationGroup, wg *sync.WaitGroup, opsChannel chan FileOperation) {
	// Decrement the WaitGroup task counter by 1 when the processReplicationGroup function returns
	defer wg.Done()

	// filepaths holds the full path of every file identified in the first of the configured directories.
	// For best performance, the first listed directory in the RG config should be the one with the most churn (the "working" storage device).
	var filepaths []string

	// Add all individual files in the directory to the filepaths array
	err := getFiles(&filepaths, rgConfig.Directories[0], true)
	if err != nil {
		log.Fatal(err)
	}

	// Parse the File Index into the list of files and their metadata (multidimensional array)
	fileIndex, err := parseFileIndex(rgConfig.FileIndex)
	if err != nil {
		log.Fatal(err)
	}

	// Create error and result channels to receive output from the opsChannel
	// These channels are reused for all file operations
	var errorChannel chan error = make(chan error, 1)
	var resultChannel chan interface{} = make(chan interface{}, 1)

	// Iterate over every file in the first directory, applying Replication Group policy to it by performing the following steps:
	// a) Collect the locations of all instances of the file in the directories (including the first directory) based on its relative path
	// b) Check for and handle any hash conflicts among the files present in the directories
	// c) Handle updates to the file index multidimensional array
	// d) Resolve any missing files
	for _, f := range filepaths {
		// Step 1a - collect locations of all instances of target file "f"
		var fileInstances []string = make([]string, 0) // fileInstances holds just the instances of this current file

		// Get the name and relative path of the current file (without the root directory path or file name)
		var fileName string = strings.Split(f, "\\")[len((strings.Split(f, "\\")))-1]
		var relativePath string = strings.Split(filepath.Dir(f), rgConfig.Directories[0])[1] + "\\"

		// Determine if any other instances of this file exist in the other configured RG directories
		for _, d := range rgConfig.Directories {
			var directoryFiles []string = make([]string, 0) // directoryFiles holds every file found in the same directory as the current file
			var searchDirectory string = d + relativePath   // the subdirectory within the current RG directory to search
			err = getFiles(&directoryFiles, searchDirectory, false)

			// Check for a matching file in the directory
			for _, e := range directoryFiles {
				// If there is a matching instance of the file, it will be at the same relative location in the different root directory
				var expectedFile string = d + relativePath + fileName
				if e == expectedFile {
					fileInstances = append(fileInstances, e)
					break // file has been found, so break iteration through remaining files
				}
			}
		}

		// Step 1b - check for and handle any hash conflicts among the files
		var fileHashes []string = make([]string, 0) // fileInstances holds the hashes of the file instances
		for _, i := range fileInstances {
			// Send the file hash request to the opsChannel for processing by fileController
			// processReplicationGroup blocks here until fileController receives the operation
			opsChannel <- FileOperation{
				Operation: "hash",
				Filepath:  i,
				Result:    resultChannel,
				Error:     errorChannel,
			}

			// processReplicationGroup then blocks here until fileController writes to the error channel (processing is complete)
			err := <-errorChannel
			if err != nil {
				log.Printf("Hash error for %s: %v", f, err)
				continue
			}

			// If the error channel is nil, then the hash can be found in the result channel; append to the fileHashes slice
			var hash = <-resultChannel
			fileHashes = append(fileHashes, hash.(string)) // ".(string)" performs a type assertion of the interface to a string
		}

		// If at least one of the files is different from the others, process the conflict(s) before proceeding
		if !sliceElementsEqual(fileHashes) {
			// NEEDS DEVELOPMENT
		}

		// Step 1c - update File Index
		// Iterate of the file index to get the entry for the current file
		var matchFound bool = false
		for ne, e := range fileIndex { // "ne" = number of "e"
			if e[0] == relativePath+fileName && e[2] == "latest" {
				// If file and file index hashes do not match, act based on the VersionConflictAction
				// The fileHashes slice remains current with the state of the filesystem, so upon reaching this step, file hashes should all be equal
				if e[1] != fileHashes[0] {
					if rgConfig.VersionConflictAction == "updateFileIndex" {
						fileIndex[ne][1] = fileHashes[0]
					} else if rgConfig.VersionConflictAction == "ask" {
						var input string
						fmt.Printf("Version conflict found with file '%s'\n", relativePath+fileName)
						fmt.Printf("\tFile hash: %s\n", e[1])
						fmt.Printf("\tFile Index hash: %s\n", fileHashes[0])
						fmt.Printf("\tHow would you like to proceed? Please enter 'updateFileIndex' or 'ignore'.\n")
						fmt.Print("> ")
						_, err = fmt.Scanln(&input)
						if err != nil {
							log.Fatal(err)
						}

						if input == "updateFileIndex" {
							fileIndex[ne][1] = fileHashes[0]
						} else if input != "ignore" {
							log.Fatalf("Invalid input: '%s'", input)
						}
					} else if rgConfig.VersionConflictAction != "never" {
						log.Fatalf("Invalid configuration VersionConflictAction value for Replication Group '%s': %s", rgConfig.Name, rgConfig.VersionConflictAction)
					}
				}

				// Match was found, stop checking
				matchFound = true
				break
			}
		}

		// If there is not an entry for this file in the File Index, or it has no "latest" version, append an entry
		if !matchFound && rgConfig.VersionConflictAction == "updateFileIndex" {
			fileIndex = append(fileIndex, []string{relativePath + fileName, fileHashes[0], "latest"})
		} else if !matchFound && rgConfig.VersionConflictAction == "ask" {
			var input string
			fmt.Printf("No File Index entry found for file '%s' latest version\n", relativePath+fileName)
			fmt.Printf("\tHow would you like to proceed? Please enter 'addEntry' or 'ignore'.\n")
			fmt.Print("> ")
			_, err = fmt.Scanln(&input)
			if err != nil {
				log.Fatal(err)
			}

			if input == "addEntry" {
				fileIndex = append(fileIndex, []string{relativePath + fileName, fileHashes[0], "latest"})
			} else if input != "ignore" {
				log.Fatalf("Invalid input: '%s'", input)
			}
		} else if !matchFound && rgConfig.VersionConflictAction != "never" {
			log.Fatalf("Invalid configuration VersionConflictAction value for Replication Group '%s': %s", rgConfig.Name, rgConfig.VersionConflictAction)
		}
	}

	// Step 3 - rewrite the File Index file with the changes to the fileIndex multidimensional array
	err = writeFileIndex(rgConfig.FileIndex, fileIndex)
	if err != nil {
		log.Fatal(err)
	}
}

func main() {
	// Startup output
	const banner = `
  _____ _  _         ____            _ _            _             
 |  ___(_)| | ___   |  _ \ ___ _ __ | (_) ___ __ _| |_ ___  _ __ 
 | |_  | || |/ _ \  | |_) / _ \ '_ \| | |/ __/ _' | __/ _ \| '__|
 |  _| | || <  __/  |  _ <  __/ |_) | | | (_| (_| | || (_) | |   
 |_|   |_||_|\___|  |_| \_\___| .__/|_|_|\___\__,_|\__\___/|_|   
                              |_|                                 
`

	currentDirectory, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\n%s\n\n", banner)
	fmt.Printf("Currently running in directory %s\n", currentDirectory)

	// Config file is loaded on startup and can be rechecked by relaunching the application
	// Default to config.json in the root directory
	config, err := parseConfig("config.json")
	if err != nil {
		log.Fatal(err)
	}

	// Create the file operations channel and start the fileController goroutine
	// The operations channel is used as an interface for the processReplicationGroup goroutines to communicate with fileController
	opsChannel := make(chan FileOperation)
	go fileController(opsChannel)

	// Create a WaitGroup to prevent early application termination until all Replication Groups are finished processing, then start concurrent RG processing
	var wg sync.WaitGroup
	for _, rgConfig := range config.ReplicationGroups { // range on a slice outputs the index of the current element (ignored) and the element itself
		wg.Add(1) // increment the WaitGroup by 1 for each ReplicationGroup
		go processReplicationGroup(rgConfig, &wg, opsChannel)
	}

	// Block return of the main function until all required goroutines complete
	wg.Wait()
}
