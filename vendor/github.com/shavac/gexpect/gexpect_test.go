package gexpect

import (
	"fmt"
	"github.com/davecgh/go-spew/spew"
	"testing"
)

func TestPlan(t *testing.T) {
	sshPlan := NewPlan()
	loginStep := sshPlan.Flow.Expect(5, "[Uu]sername:")
	loginStep.WhenTimeout.Terminate("username timeout")
	loginStep.WhenMatched.VarSendLine("username")
	passStep := loginStep.WhenMatched.Expect(5, "[Pp]assword:")
	passStep.WhenTimeout.Terminate("password timeout.")
	passStep.WhenMatched.VarSendLine("password")
	shellPromptStep := passStep.WhenMatched.Expect(5, "[$#>]")
	shellPromptStep.WhenMatched.Interact()

	sshPlan.BindVar("username", "knightmare")
	sshPlan.BindVar("password", "pass")
	if err := sshPlan.VarApply(); err != nil {
		fmt.Println(err)
	}
	spew.Dump(sshPlan)
}
