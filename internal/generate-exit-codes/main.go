//
// DISCLAIMER
//
// Copyright 2017-2022 ArangoDB GmbH, Cologne, Germany
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Copyright holder is ArangoDB GmbH, Cologne, Germany
//

package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/pkg/errors"
)

const (
	exitCodesDat = "https://raw.githubusercontent.com/arangodb/arangodb/main/lib/Basics/exitcodes.dat"
)

type exitCode struct {
	code        int
	reason      string
	description string
}

func fatal(args ...interface{}) {
	fmt.Print(args...)
	os.Exit(1)
}

func main() {
	root, err := os.Getwd()
	if err != nil {
		fatal(err)
		return
	}

	dat, err := downloadArangodExitCodesDat()
	if err != nil {
		fatal(err)
		return
	}

	exitCodes, err := parseArangoDExitCodes(string(dat))
	if err != nil {
		fatal(err)
		return
	}

	err = generateExitCodesGoSource(exitCodes, root, err)
	if err != nil {
		fatal(err)
		return
	}

	fmt.Printf("ArangoD exit codes consts generated. Total %d codes found\n", len(exitCodes))
}

func generateExitCodesGoSource(exitCodes map[string]exitCode, root string, err error) error {
	header, err := getLicenseHeader(root)
	if err != nil {
		return err
	}

	buf := bytes.Buffer{}
	buf.WriteString(header)
	buf.WriteString(`
// Code generated automatically. DO NOT EDIT.

package definitions

const (
`)
	for name, code := range exitCodes {
		buf.WriteString(fmt.Sprintf("	// %s\n", name))
		buf.WriteString(fmt.Sprintf(`	%s = %d // %s
`, getConstName(name), code.code, code.description))
	}
	buf.WriteString(")\n\n")

	buf.WriteString("var arangoDExitReason = map[int]string{\n")
	for name, code := range exitCodes {
		buf.WriteString(fmt.Sprintf("	// %s\n", name))
		buf.WriteString(fmt.Sprintf("	%s: \"%s\",\n", getConstName(name), code.reason))
	}
	buf.WriteString("}\n")

	resultFilePath := fmt.Sprintf("%s/pkg/definitions/exitcodes_generated.go", root)
	err = ioutil.WriteFile(resultFilePath, buf.Bytes(), 0600)
	return err
}

func getConstName(n string) string {
	prev := '_'
	// convert SOME_CONST_NAME to SomeConstName:
	dropUnderscoreAndStringify := func(r rune) rune {
		if prev == '_' {
			prev = r
			return unicode.ToTitle(r)
		}
		prev = r
		if r == '_' {
			return -1
		}
		return r
	}
	return "ArangoD" + strings.Map(dropUnderscoreAndStringify, strings.ToLower(n))
}

func downloadArangodExitCodesDat() ([]byte, error) {
	resp, err := http.Get(exitCodesDat)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return ioutil.ReadAll(resp.Body)
}

func parseArangoDExitCodes(dat string) (map[string]exitCode, error) {
	// omit comments
	lines := strings.Split(dat, "\n")
	dat = ""
	for _, line := range lines {
		parts := strings.Split(line, "#")
		if len(parts[0]) > 0 {
			dat += parts[0] + "\n"
		}
	}

	b := bytes.NewBufferString(dat)
	csvReader := csv.NewReader(b)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}
	result := make(map[string]exitCode, len(records))
	for _, r := range records {
		if len(r) < 4 {
			return nil, fmt.Errorf("expected at least 4 fields, got: %+v", r)
		}
		code, err := strconv.Atoi(r[1])
		if err != nil {
			return nil, errors.Wrapf(err, "while converting %s", r[1])
		}
		result[r[0]] = exitCode{
			code:        code,
			reason:      r[2],
			description: r[3],
		}
	}
	return result, nil
}

func getLicenseHeader(root string) (string, error) {
	headerBoilerplate := fmt.Sprintf("%s/LICENSE.BOILERPLATE", root)
	b, err := ioutil.ReadFile(headerBoilerplate)
	if err != nil {
		return "", err
	}
	result := ""
	for _, line := range strings.Split(string(b), "\n") {
		if len(line) > 0 {
			result += "// " + line + "\n"
		} else {
			result += "//\n"
		}
	}
	return result, nil
}
