package service

import (
	"fmt"
	"io"
	"strings"
)

var confHeader = `# ArangoDB configuration file
#
# Documentation:
# https://docs.arangodb.com/Manual/Administration/Configuration/
#

`

type configFile []*configSection

// WriteTo writes the configuration sections to the given writer.
func (cf configFile) WriteTo(w io.Writer) (int64, error) {
	x := int64(0)
	n, err := w.Write([]byte(confHeader))
	if err != nil {
		return x, maskAny(err)
	}
	x += int64(n)
	for _, section := range cf {
		n, err := section.WriteTo(w)
		if err != nil {
			return x, maskAny(err)
		}
		x += int64(n)
	}
	return x, nil
}

type configSection struct {
	Name     string
	Settings map[string]string
}

// WriteTo writes the configuration section to the given writer.
func (s *configSection) WriteTo(w io.Writer) (int64, error) {
	lines := []string{"[" + s.Name + "]"}
	for k, v := range s.Settings {
		lines = append(lines, fmt.Sprintf("%s = %s", k, v))
	}
	lines = append(lines, "")
	n, err := w.Write([]byte(strings.Join(lines, "\n")))
	return int64(n), maskAny(err)
}
