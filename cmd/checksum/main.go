package main

import (
	"fmt"
	"os"

	"example.poc/device-monitoring-system/test/helper"
)

// A simple program to simulate the external checksum generator binary
func main() {
	fmt.Fprintln(os.Stdout, helper.RandomString(32))
}
