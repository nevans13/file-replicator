package main

import (
	"fmt"
	"log"
	"os"
)

// The config is scoped globally
type Config struct {
	Directories []string
}

func parseConfig(configFile string) {
}

func main() {
	// Config file is loaded on startup and can be rechecked by relaunching the application
	// Default to config.json in the root directory as the configuration file
	parseConfig("config.json")

	files, err := os.ReadDir(".")
	if err != nil {
		log.Fatal(err)
	}

	for _, f := range files {
		fmt.Println(f.Name())
	}
}
