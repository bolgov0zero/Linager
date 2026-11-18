//go:build ignore

// gen turns the IEEE registry of MA-L assignments (https://standards-oui.ieee.org/oui/oui.csv) into oui.tsv.gz: one
// line "AABBCC<TAB>Organization" per assignment, sorted, compressed. Run it from this directory:
//
//	go run gen.go /path/to/oui.csv
package main

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"os"
	"sort"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run gen.go oui.csv")
		os.Exit(2)
	}
	f, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer func() { _ = f.Close() }()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var lines []string
	for i, row := range rows {
		if i == 0 || len(row) < 3 || row[0] != "MA-L" || len(row[1]) != 6 {
			continue
		}
		name := strings.Join(strings.Fields(row[2]), " ")
		if name == "" || strings.EqualFold(name, "private") {
			continue
		}
		lines = append(lines, strings.ToUpper(row[1])+"\t"+name)
	}
	sort.Strings(lines)
	out, err := os.Create("oui.tsv.gz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	zw, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	for _, l := range lines {
		_, _ = zw.Write([]byte(l + "\n"))
	}
	_ = zw.Close()
	_ = out.Close()
	fmt.Println(len(lines), "assignments")
}
