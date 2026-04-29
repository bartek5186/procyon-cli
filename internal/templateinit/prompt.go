package templateinit

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type choice struct {
	Value string
	Label string
}

func prompt(reader *bufio.Reader, out io.Writer, label, fallback string) string {
	if fallback != "" {
		fmt.Fprintf(out, "%s [%s]: ", label, fallback)
	} else {
		fmt.Fprintf(out, "%s: ", label)
	}

	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback
	}
	return line
}

func promptChoice(reader *bufio.Reader, out io.Writer, label string, choices []choice) string {
	fmt.Fprintf(out, "%s:\n", label)
	for i, choice := range choices {
		fmt.Fprintf(out, "  %d. %s\n", i+1, choice.Label)
	}

	for {
		fmt.Fprintf(out, "Select [1]: ")
		line, _ := reader.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return choices[0].Value
		}
		idx, err := strconv.Atoi(line)
		if err == nil && idx >= 1 && idx <= len(choices) {
			return choices[idx-1].Value
		}
		for _, choice := range choices {
			if line == choice.Value {
				return choice.Value
			}
		}
		fmt.Fprintf(out, "Invalid choice.\n")
	}
}
