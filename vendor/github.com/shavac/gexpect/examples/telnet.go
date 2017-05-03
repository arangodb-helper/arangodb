package main

import (
	"../../gexpect"
	"fmt"
	"regexp"
	"time"
)

func main() {
	child, _ := gexpect.NewSubProcess("/usr/bin/telnet", "bbs.newsmth.com")
	child.Echo()
	if err := child.Start(); err != nil {
		fmt.Println(err)
	}
	defer child.Close()
	r := regexp.MustCompile(":")
	idx, _ := child.ExpectTimeout(5*time.Second, r)
	if idx >= 0 {
		child.SendLine("knightmare")
	}
	if idx, _ := child.ExpectTimeout(5*time.Second, regexp.MustCompile(":")); idx >= 0 {
		child.SendLine("shava123")
	}
	child.InteractTimeout(1 * time.Minute)
}
