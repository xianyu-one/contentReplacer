package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

func replaceContentInFiles(rootDir string, extensions []string, find string, replace string) error {
	err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			for _, ext := range extensions {
				if strings.HasSuffix(info.Name(), "."+ext) {
					data, err := ioutil.ReadFile(path)
					if err != nil {
						return err
					}
					content := string(data)
					newContent := strings.ReplaceAll(content, find, replace)
					if newContent != content {
						err = ioutil.WriteFile(path, []byte(newContent), 0644)
						if err != nil {
							return err
						}
						fmt.Printf("Updated file: %s\n", path)
					}
					break
				}
			}
		}
		return nil
	})
	return err
}

func main() {
	rootDir := flag.String("p", ".", "Specify target folder")
	extensions := flag.String("fe", "md,txt", "Specify file extensions")
	find := flag.String("r", "", "Specify the original content to find")
	replace := flag.String("t", "", "Specify the content to replace with")

	flag.Parse()

	exts := strings.Split(*extensions, ",")

	err := replaceContentInFiles(*rootDir, exts, *find, *replace)
	if err != nil {
		fmt.Println("Error:", err)
	}
}
