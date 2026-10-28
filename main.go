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

type ReplicationGroup struct {
	Name        string   // name of the Replication Group
	FileIndex   string   // name of the CSV file that holds the file index
	Directories []string // one or more directories to include in the Replication Group's context
}

// The structure of this struct must match the JSON structure of the config file
type Config struct {
	ReplicationGroups []ReplicationGroup
}

// Parse the configuration file, returning the config as an object
func parseConfig(configFile string) (Config, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return Config{}, err
	}

	var config Config
	err = json.Unmarshal(data, &config)
	if err != nil {
		return Config{}, fmt.Errorf("Failed to parse config: %w", err)
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

// This is the root function, called as goroutines, that handles the processing for a specific Replication Group
// Each Replication Group is handled by exactly one processReplicationGroup goroutine, and each goroutine processes only one Replication Group
// Each instance of this function is responsible for its own downstream processing (error handling, reporting, etc)
// The fileCoordinator function coordinates access to files among the processReplicationGroup goroutines
func processReplicationGroup(rgConfig ReplicationGroup, wg *sync.WaitGroup) {
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
		var hash, _ = getFileHash(h)
		fmt.Println(h)
		fmt.Println(hash)
		fmt.Println()
	}
}

func main() {
	// Config file is loaded on startup and can be rechecked by relaunching the application
	// Default to config.json in the root directory
	config, err := parseConfig("config.json")
	if err != nil {
		log.Fatal(err)
	}

	// Start the fileCoordinator goroutine

	// Create a WaitGroup to prevent early application termination until all Replication Groups are finished processing
	var wg sync.WaitGroup

	// Start concurrent processing for each Replication Group
	for _, rgConfig := range config.ReplicationGroups { // range outputs the index of the current element (ignored) and the element itself
		wg.Add(1) // increment the WaitGroup by 1 for each ReplicationGroup
		go processReplicationGroup(rgConfig, &wg)
	}

	// Block return of the main function until all required goroutines complete
	wg.Wait()
}
