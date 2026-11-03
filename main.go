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

type Metadata struct {
	Description []string // a description of the configuration file that is ignored by the application
}

type ReplicationGroup struct {
	Name            string   // name of the Replication Group
	FileIndex       string   // name of the CSV file that holds the file index
	ConflictAction  string   // action to take for version conflicts
	DisplayFileDiff string   // settings for displaying the diff for supported file types
	Directories     []string // one or more directories to include in the Replication Group's context
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
func contains[T comparable](s []T, e T) bool {
	for _, i := range s {
		if i == e {
			return true
		}
	}
	return false
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
		if contains(replicationGroupNames, r.Name) {
			return Config{}, fmt.Errorf("Replication Group name '%s' in %s is not unique", r.Name, configFile)
		}
		replicationGroupNames = append(replicationGroupNames, r.Name)
	}

	// Validate that there are no duplicate Replication Group file indexes
	var replicationGroupFileIndexes []string
	for _, r := range config.ReplicationGroups {
		if contains(replicationGroupFileIndexes, r.FileIndex) {
			return Config{}, fmt.Errorf("Replication Group file index '%s' in %s is not unique", r.FileIndex, configFile)
		}
		replicationGroupFileIndexes = append(replicationGroupFileIndexes, r.FileIndex)
	}

	return config, nil
}

// Given the path to the File Index, return a structured list of files and their metadata
func parseFileIndex(f string) ([][]string, error) {
	// Define the target header row of the File Index CSV
	const headerRow string = "filename,hash,version"

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
				return [][]string{}, fmt.Errorf("Could not write fileIndex header: %e", err)
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

// Get the full filepaths for every file in the configured directories, appending them to the provided slice
// Called recursively to iterate over all sub-directories
// Fuction takes a pointer to the filepaths slice (declared in main function) and the target directory (full path) to iterate over
func getFiles(filepaths *[]string, directory string) error {
	files, err := os.ReadDir(directory)
	if err != nil {
		return err
	}

	for _, f := range files {
		if f.IsDir() {
			err = getFiles(filepaths, filepath.Join(directory, f.Name())) // recursive call with the filepaths pointer
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
// 2) Interate through each of the other directories in the RG, handling any files present that were not present in the initial loop (step 1)
// The fileController function coordinates access to files among the processReplicationGroup goroutines, and communication is handled by the opsChannel.
func processReplicationGroup(rgConfig ReplicationGroup, wg *sync.WaitGroup, opsChannel chan FileOperation) {
	// Decrement the WaitGroup task counter by 1 when the processReplicationGroup function returns
	defer wg.Done()

	// filepaths holds the full path of every file identified in the first of the configured directories.
	// For best performance, the first listed directory in the RG config should be the one with the most churn (the "working" storage device).
	var filepaths []string

	// Add all individual files in the directory to the filepaths array
	err := getFiles(&filepaths, rgConfig.Directories[0])
	if err != nil {
		log.Fatal(err)
	}

	// Parse the File Index into the list of files and their metadata (multidimensional array)
	_, err = parseFileIndex(rgConfig.FileIndex)
	if err != nil {
		log.Fatal(err)
	}

	// Create error and result channels to receive output from the opsChannel
	// These channels are reused for all file operations
	var errorChannel chan error = make(chan error, 1)
	var resultChannel chan interface{} = make(chan interface{}, 1)

	// Iterate over every file, applying Replication Group policy to it
	for _, f := range filepaths {
		// Send the file hash request to the opsChannel for processing by fileController
		// processReplicationGroup blocks here until fileController receives the operation
		opsChannel <- FileOperation{
			Operation: "hash",
			Filepath:  f,
			Result:    resultChannel,
			Error:     errorChannel,
		}

		// processReplicationGroup then blocks here until fileController writes to the error channel (processing is complete)
		err := <-errorChannel
		if err != nil {
			log.Printf("Hash error for %s: %v", f, err)
			continue
		}

		// If the error channel is nil, then the hash can be found in the result channel
		var hash = <-resultChannel

		fmt.Println(hash) // TEMPORARY
	}
}

func main() {
	// Startup output
	currentDirectory, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Starting file-replicator")
	fmt.Printf("Currently running in directory %s", currentDirectory)

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
