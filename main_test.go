package main_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/go-delve/delve/service/api"
	"github.com/go-delve/delve/service/rpc2"
)

func TestFunctions(t *testing.T) {
	cmd := exec.Command("dlv", "--headless", "exec", "helloworld", "-l", "127.0.0.1:4112")
	cmd.Dir = "/Users/philipp/code/hellworld"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Start()

	time.Sleep(time.Second)

	c := rpc2.NewClient("127.0.0.1:4112")
	c.CreateBreakpoint(&api.Breakpoint{FunctionName: "main.main"})
	state := <-c.Continue()

	frames := must(c.Stacktrace(state.CurrentThread.GoroutineID, 100, api.StacktraceSimple, nil))
	for _, f := range frames {
		fmt.Println(f.Bottom, f.Location.PC)
	}
}
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
