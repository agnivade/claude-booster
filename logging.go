package main

import "fmt"

const (
	colorReset  = "\033[0m"
	colorBlue   = "\033[34m"
	colorGreen  = "\033[32m"
	colorRed    = "\033[31m"
	colorYellow = "\033[33m"
)

func printBlue(format string, args ...interface{}) {
	fmt.Printf(colorBlue+format+colorReset, args...)
}

func printGreen(format string, args ...interface{}) {
	fmt.Printf(colorGreen+format+colorReset, args...)
}

func printRed(format string, args ...interface{}) {
	fmt.Printf(colorRed+format+colorReset, args...)
}

func printYellow(format string, args ...interface{}) {
	fmt.Printf(colorYellow+format+colorReset, args...)
}
