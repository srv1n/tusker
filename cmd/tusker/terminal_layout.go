package main

import (
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/term"
)

const (
	defaultTerminalWidth = 100
	minTerminalWidth     = 40
)

func terminalOutputWidth(args Args) int {
	if width := terminalWidthArg(args.String("width")); width > 0 {
		return width
	}
	if width := terminalWidthArg(os.Getenv("TUSKER_WIDTH")); width > 0 {
		return width
	}
	if width, _, err := term.GetSize(os.Stdout.Fd()); err == nil && width > 0 {
		return clampTerminalWidth(width)
	}
	if width := terminalWidthArg(os.Getenv("COLUMNS")); width > 0 {
		return width
	}
	return defaultTerminalWidth
}

func terminalWidthArg(value string) int {
	columns, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || columns <= 0 {
		return 0
	}
	return clampTerminalWidth(columns)
}

func clampTerminalWidth(width int) int {
	if width < minTerminalWidth {
		return minTerminalWidth
	}
	return width
}

func displayCellWidth(value string) int {
	return lipgloss.Width(value)
}
