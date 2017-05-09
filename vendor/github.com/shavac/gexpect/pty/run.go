package pty

import (
	"os/exec"
	"syscall"
	"errors"
	"fmt"
)

// Start assigns a pseudo-terminal tty os.File to c.Stdin, c.Stdout,
// and c.Stderr, calls c.Start, and returns the File of the tty's
// corresponding pty.
func Start(c *exec.Cmd) (term *Terminal, err error) {
	if term, err = NewTerminal(); err != nil {
		return nil, err
	}
	return term, term.Start(c)
}

func (t *Terminal) Start(c *exec.Cmd) (err error) {
	if t == nil {
		return errors.New("terminal not assigned.")
	}
	//defer t.Tty.Close()
	c.Stdout = t.Tty
	c.Stdin = t.Tty
	c.Stderr = t.Tty
	c.SysProcAttr = &syscall.SysProcAttr{Setctty: true, Setsid: true}
	if err = c.Start() ; err != nil {
		fmt.Println("error is ",err)
		t.Pty.Close()
		return
	}
	return
}
