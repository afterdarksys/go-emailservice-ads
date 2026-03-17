package mailtest

import (
	"fmt"
	"strings"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorCyan   = "\033[36m"
)

func printHeader(title string) {
	fmt.Printf("\n%s%s%s\n", colorBlue, title, colorReset)
	fmt.Println(strings.Repeat("=", len(title)))
}

func printInfo(format string, args ...interface{}) {
	fmt.Printf("%s%s%s\n", colorCyan, fmt.Sprintf(format, args...), colorReset)
}

func printSuccess(format string, args ...interface{}) {
	fmt.Printf("%s%s%s\n", colorGreen, fmt.Sprintf(format, args...), colorReset)
}

func printError(format string, args ...interface{}) error {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("%s✗ %s%s\n", colorRed, msg, colorReset)
	return fmt.Errorf(msg)
}

func printWarning(format string, args ...interface{}) {
	fmt.Printf("%s⚠ %s%s\n", colorYellow, fmt.Sprintf(format, args...), colorReset)
}

func printProtocol(direction, message string) {
	message = strings.TrimSpace(message)
	if direction == "<" {
		fmt.Printf("%s%s %s%s\n", colorYellow, direction, message, colorReset)
	} else {
		fmt.Printf("%s%s %s%s\n", colorCyan, direction, message, colorReset)
	}
}
