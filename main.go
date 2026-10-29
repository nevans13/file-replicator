package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type Metadata struct {
	Description []string // a description of the configuration file that is ignored by the application
}

type ReplicationGroup struct {
	Name        string   // name of the Replication Group
	FileIndex   string   // name of the CSV file that holds the file index
	Directories []string // one or more directories to include in the Replication Group's context
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
			op.Error <- err

		// Error if an invalid file operation is specified
		default:
			op.Error <- fmt.Errorf("Unknown file operation '%s'", op.Operation)
		}
	}
}

// This is the root function that handles the processing for a specific Replication Group, and it is called as a goroutine
// Each Replication Group is handled by exactly one processReplicationGroup goroutine, and each goroutine processes only one Replication Group
// Each instance of this function is responsible for its own downstream processing (error handling, reporting, user interaction, etc)
// The fileController function coordinates access to files among the processReplicationGroup goroutines, and communication is handled by the opsChannel
func processReplicationGroup(rgConfig ReplicationGroup, wg *sync.WaitGroup, opsChannel chan FileOperation) {
	// Decrement the WaitGroup task counter by 1 when the processReplicationGroup function returns
	defer wg.Done()

	// filepaths holds the full path of every file identified in the configured directories
	var filepaths []string

	// Add all individual files in the directory to the filepaths array
	for _, d := range rgConfig.Directories {
		err := getFiles(&filepaths, d)
		if err != nil {
			log.Fatal(err)
		}
	}

	// Temp testing of hashes
	for _, h := range filepaths {
		// Create error and result channels to receive output from the opsChannel
		// These channels are made new each iteration of the loop, and the old ones are marked for garbage collection
		errorChannel := make(chan error, 1)
		resultChannel := make(chan interface{}, 1)

		// Send the file hash request to the opsChannel for processing by fileController
		// processReplicationGroup blocks here until fileController receives the operation
		opsChannel <- FileOperation{
			Operation: "hash",
			Filepath:  h,
			Result:    resultChannel,
			Error:     errorChannel,
		}

		// processReplicationGroup then blocks here until fileController writes to the error channel (processing is complete)
		err := <-errorChannel
		if err != nil {
			log.Printf("Hash error for %s: %v", h, err)
			continue
		}

		var hash = <-resultChannel
		fmt.Println(h)
		fmt.Println(hash)
		fmt.Println()
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
