package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
)

// The structure of this struct must match the JSON structure of the config file
type Config struct {
	Directories []string // one or more directories to include in the application's context
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

func main() {
	// Config file is loaded on startup and can be rechecked by relaunching the application
	// Default to config.json in the root directory as the configuration file
	config, err := parseConfig("config.json")
	if err != nil {
		log.Fatal(err)
	}

	for i := 0; i < len(config.Directories); i++ {
		fmt.Println(config.Directories[i])
	}

	files, err := os.ReadDir(".")
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		fmt.Println(f.Name())
	}
}
