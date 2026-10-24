package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "-version" {
		fmt.Fprintln(os.Stderr, `openjdk version "25.0.1" 2025-10-21`)
		return
	}
	mode, _ := os.ReadFile("fixture-mode")
	fmt.Fprintln(os.Stderr, "fixture stderr captured")
	if strings.TrimSpace(string(mode)) == "crash" {
		os.Exit(9)
	}
	fmt.Println(`Done (0.01s)! For help, type "help"`)
	if strings.TrimSpace(string(mode)) == "ignore-stop" {
		for {
			time.Sleep(time.Second)
		}
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		if scanner.Text() == "stop" {
			_ = os.WriteFile("stopped.marker", []byte("graceful"), 0600)
			return
		}
	}
}
