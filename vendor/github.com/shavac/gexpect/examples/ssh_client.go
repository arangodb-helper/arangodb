package main

import (
	"../../gexpect"
	"log"
	"os"
	"os/exec"
	"regexp"
	"time"
)

// gexpect SSH/SCP client
type client struct {
	host     string
	user     string
	password string
}

var ask_known_hosts = regexp.MustCompile(`Are you sure you want to continue connecting (yes/no)?`)
var ask_password = regexp.MustCompile(`password:`)

// new client
func NewClient(host, user, password string) *client {
	return &client{host, user, password}
}

func (c *client) Download(remote, local string) error {
	scp, err := exec.LookPath("scp")
	if err != nil {
		return err
	}
	child, err := gexpect.NewSubProcess(scp, c.user+"@"+c.host+":"+remote, local)
	if err != nil {
		return err
	}
	return c.expect(child, 0, 0)
}

func (c *client) Upload(local, remote string) error {
	scp, err := exec.LookPath("scp")
	if err != nil {
		return err
	}
	child, err := gexpect.NewSubProcess(scp, local, c.user+"@"+c.host+":"+remote)
	if err != nil {
		return err
	}
	return c.expect(child, 0, 0)
}

func (c *client) RemoteExec(cmd string) error {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		return err
	}
	child, err := gexpect.NewSubProcess(ssh, c.user+"@"+c.host, cmd)
	if err != nil {
		return err
	}
	return c.expect(child, 0, 0)
}

func (c *client) expect(child *gexpect.SubProcess, expectTimeoutSec, interactTimeoutSec time.Duration) error {
	defer child.Close()
	if err := child.Start(); err != nil {
		return err
	}
	if idx, _ := child.ExpectTimeout(expectTimeoutSec*time.Second, ask_known_hosts, ask_password); idx >= 0 {
		if idx == 0 {
			child.SendLine("yes")
			if idx, _ := child.ExpectTimeout(expectTimeoutSec*time.Second, ask_password); idx >= 0 {
				child.SendLine(c.password)
			}
		} else if idx == 1 {
			child.SendLine(c.password)
		}
	}
	return child.InteractTimeout(interactTimeoutSec * time.Second)
}

func main() {
	remote_host := "localhost"
	remote_user := "user"
	password := "password"

	c := NewClient(remote_host, remote_user, password)

	file := "/tmp/dummy_file"
	downloadedFile := "/tmp/downloaded_file"
	uploadedFile := "/tmp/uploaded_file"

	println("\n")
	log.Printf("Creating file %s on the host \"%s\"...", file, remote_host)
	if err := c.RemoteExec("touch " + file); err != nil {
		log.Fatalf("ERROR: creating file %s on the host \"%s\" : %s", file, remote_host, err.Error())
	}
	log.Printf("The file %s is created on the host \"%s\"", file, remote_host)

	log.Printf("Downloading file %s form the host %s", file, remote_host)
	if err := c.Download(file, downloadedFile); err != nil {
		log.Fatalf("ERROR: downloading file %s from the host \"%s\" : %s", file, remote_host, err.Error())
	}
	log.Printf("The file %s is downloaded.Local path: %s", file, downloadedFile)

	log.Printf("Uploading file %s to the host \"%s\"", file, remote_host)
	if err := c.Upload(file, uploadedFile); err != nil {
		log.Fatalf("ERROR: uploading file %s to the host \"%s\"", file, remote_host)
	}
	log.Printf("The file %s is uploaded.Location on remote host: %s\n\n", file, uploadedFile)

	for _, file := range []string{file, downloadedFile, uploadedFile} {
		os.Remove(file)
	}
}
