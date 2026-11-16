// Command greeter prints a greeting for each argument.
package main

import (
	"fmt"
	"os"

	"example.com/greeter"
)

func main() {
	for _, name := range os.Args[1:] {
		msg, err := greeter.Greet(name, greeter.Plain)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(msg)
	}
}
