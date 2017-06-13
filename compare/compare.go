package main

import (
	"os"
	"fmt"
	"log"
	"bufio"
	"strings"
)

var knownAggs = map[string]string {
	"2e662fd5-a9cc-42d8-a85a-ac2eb75827f6":"dc seeded app",
}

func aggIdFromKey(key string) string {
	parts := strings.Split(key,":")
	if len(parts) == 4 {
		return parts[2]
	} else {
		return ""
	}
}

func dumpMap(fileName string) map[string]string{
	agg2DataMap := make(map[string]string)
	recNo := 1

	file, err := os.Open(fileName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		lineParts := strings.Split(line," ")
		if len(lineParts) != 2 {
			fmt.Println("skipping", line)
		}

		val,ok := agg2DataMap[lineParts[0]]
		if ok {
			fmt.Println("Map already has entry for", lineParts[0])
			fmt.Println("\thas", val)
			fmt.Println("\tadding", lineParts[1])
			fmt.Println("\t...at record", recNo)

			aggId := aggIdFromKey(lineParts[0])
			desc, ok := knownAggs[aggId]
			if ok {
				fmt.Println("\t... => ", desc)
			}
		}

		agg2DataMap[lineParts[0]] = lineParts[1]
		recNo += 1
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	return agg2DataMap
}

func main() {
	if len(os.Args) != 3 {
		fmt.Printf("Usage: %s\n <src dump> <target dump>", os.Args[0])
		os.Exit(1)
	}

	src := os.Args[1]
	target := os.Args[2]

	fmt.Printf("Source dump: %s, target dump: %s\n", src, target)

	fmt.Println("Make source map")
	srcMap := dumpMap(src)

	fmt.Println("Make target map")
	targetMap := dumpMap(target)

	fmt.Printf("%d source records, %d target records", len(srcMap), len(targetMap))
}
