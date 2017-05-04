package main

import (
	"../../gexpect"
	"fmt"
	"regexp"
	"time"
)

func main() {
	child, _ := gexpect.NewSubProcess("/usr/bin/ssh", "knightmare@localhost")
	if err := child.Start(); err != nil {
		fmt.Println(err)
	}
	defer child.Close()
	if idx, _ := child.ExpectTimeout(0*time.Second, regexp.MustCompile("password:")); idx >= 0 {
		child.SendLine("P@ssw0rd")
	}
	child.InteractTimeout(20 * time.Second)
}
