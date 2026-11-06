package pprint

import (
	"encoding/json"
	"fmt"
	"os"
)

func Print(i interface{}) {
	fmt.Printf("%+v\n", i)
}

func Log(i interface{}) string {

	return fmt.Sprintf("%+v\n", i)
}
func PrintJOSN(x interface{}) {
	ioWriter := os.Stdout
	w := json.NewEncoder(ioWriter)
	w.SetIndent("", "    ")
	err := w.Encode(x)
	if err != nil {
		fmt.Printf("Error encoding JSON: %v\n", err)
	}
}
