package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"text/template"
)

const (
	root         = "alphabet"
	templateName = "alphabet.tpl"
)

func main() {
	glagolic := []string{
		"Az", "Buky", "Vedi", "Glagoli", "Dobro", "Jest", "Zhivete",
	}

	data, err := ioutil.ReadFile(path.Join(root, templateName))
	die("can't read template file", err)

	tmpl := template.Must(template.New("").Parse(string(data)))

	for index, name := range glagolic {
		lowercaseName := strings.ToLower(name)

		if _, err := os.Stat(path.Join(root, lowercaseName)); os.IsNotExist(err) {
			os.Mkdir(path.Join(root, lowercaseName), 0755)
		}

		dst, err := os.Create(path.Join(root, lowercaseName, lowercaseName+"_contract.go"))
		die("can't create file", err)

		err = tmpl.Execute(dst, map[string]interface{}{
			"Name":  name,
			"Index": index,
		})
		die("can't generate code from template", err)

		die("can't close generated file", dst.Close())
	}

	os.Exit(0)
}

func die(msg string, err error) {
	if err != nil {
		fmt.Printf(msg+": %v\n", err)
		os.Exit(1)
	}
}
