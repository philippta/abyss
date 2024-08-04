package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/go-delve/delve/service/api"
	"github.com/go-delve/delve/service/rpc2"
)

var debuglog *os.File

func main() {
	var err error
	debuglog, err = tea.LogToFile("tea.log", "")
	if err != nil {
		panic(err)
	}

	cmd := exec.Command("dlv", "--headless", "exec", "helloworld", "-l", "127.0.0.1:4111")
	cmd.Dir = "/Users/philipp/code/hellworld"
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Start()

	time.Sleep(time.Second)

	c := rpc2.NewClient("127.0.0.1:4111")
	c.CreateBreakpoint(&api.Breakpoint{FunctionName: "main.main"})
	<-c.Continue()

	_, err = c.GetState()
	must(err)

	m := model{
		dbg: c,
	}

	_ = m
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}

type model struct {
	dbg           *rpc2.RPCClient
	width, height int

	registers     api.Registers
	prevRegisters api.Registers
	prevStack     []byte
	stack         []byte
	stackaddr     uint64
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "s":
			stepInstruction(m.dbg)
		case "i":
			m.dbg.StepInstruction(false)
		case "o":
			m.dbg.StepOut()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	}

	var err error
	state, err := m.dbg.GetState()
	must(err)

	m.prevRegisters = m.registers
	m.registers, err = m.dbg.ListScopeRegisters(api.EvalScope{GoroutineID: state.CurrentThread.GoroutineID}, false)
	must(err)

	stackaddr, err := strconv.ParseUint(m.registers[1].Value[2:], 16, 64)
	must(err)

	m.stackaddr = stackaddr
	m.prevStack = m.stack
	m.stack, _, err = m.dbg.ExamineMemory(stackaddr, 256)
	must(err)

	return m, nil
}

func (m model) View() string {
	asm := lipgloss.NewStyle().
		Width(m.width / 2).
		Height(m.height)

	paneRegisters := lipgloss.NewStyle().
		Width(m.width / 4).
		Height(m.height)

	// mem := lipgloss.NewStyle().
	// 	BorderStyle(lipgloss.NormalBorder()).
	// 	BorderRight(true).
	// 	Width(m.width / 2).
	// 	Height(15)

	return lipgloss.JoinHorizontal(
		lipgloss.Top,
		asm.Render(disassembly(m.dbg, m.height)),
		paneRegisters.Render(registers(m.registers, m.prevRegisters)),
		paneRegisters.Render(stack(m.stackaddr, m.stack, m.prevStack)),
	)
	// lipgloss.JoinHorizontal(
	// 	lipgloss.Top,
	// 	mem.Render(dummytext["stack"]),
	// ),
}

func stepInstruction(c *rpc2.RPCClient) {
	state, err := c.GetState()
	must(err)

	asms, err := c.DisassemblePC(api.EvalScope{GoroutineID: state.CurrentThread.GoroutineID}, state.CurrentThread.PC, api.IntelFlavour)
	must(err)

	var isCall bool
	for _, asm := range asms {
		if asm.AtPC {
			if strings.HasPrefix(asm.Text, "CALL ") {
				isCall = true
			}
			break
		}
	}

	_, err = c.StepInstruction(false)
	must(err)

	if isCall {
		_, err := c.StepOut()
		must(err)
	}
}

func stack(stackaddr uint64, stack, prevStack []byte) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	var sb strings.Builder
	sb.WriteString(style.Render("Stack") + "\n")
	var buf [8]byte
	var prevbuf [8]byte
	for i, b := range stack {
		if i%8 == 0 {
			sb.WriteString(fmt.Sprintf("%X    ", stackaddr+uint64(i)))
		}

		buf[7-i%8] = b

		if len(prevStack) > 0 {
			prevbuf[7-i%8] = prevStack[i]
		}
		if i%8 == 7 {
			if bytes.Equal(buf[:], prevbuf[:]) {
				sb.WriteString(fmt.Sprintf("%02X", buf))
			} else {
				sb.WriteString(style.Render(fmt.Sprintf("%02X", buf)))
			}
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func disassembly(c *rpc2.RPCClient, maxlines int) string {
	state, err := c.GetState()
	must(err)

	asms, err := c.DisassembleRange(
		api.EvalScope{GoroutineID: state.CurrentThread.GoroutineID},
		state.CurrentThread.PC-(4*(uint64(maxlines)/2)),
		state.CurrentThread.PC+(4*(uint64(maxlines)/2)),
		api.IntelFlavour)
	must(err)

	var lines []string
	var prevFunctionName string
	for _, asm := range asms {
		if asm.Loc.Function != nil && (prevFunctionName == "" || asm.Loc.Function.Name() != prevFunctionName) {
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf("%s", lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true).Render(asm.Loc.Function.Name())))
			prevFunctionName = asm.Loc.Function.Name()
		}
		var opcodes strings.Builder
		for i := len(asm.Bytes) - 1; i >= 0; i-- {
			opcodes.WriteString(fmt.Sprintf("%02X ", asm.Bytes[i]))
		}
		if asm.Text == "?" {
			asm.Text = ""
		}

		style := lipgloss.NewStyle()
		if asm.AtPC {
			style = style.Foreground(lipgloss.Color("4")).Bold(true)
		}
		lines = append(lines, fmt.Sprintf("%-16X %-18s %-30s", asm.Loc.PC, opcodes.String(), style.Render(reformatasm(asm.Text))))
	}
	if len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	if len(lines) > maxlines {
		lines = lines[:maxlines]
	}

	return strings.Join(lines, "\n")
}

func reformatasm(s string) string {
	segs := strings.SplitN(s, " ", 2)
	if len(segs) == 1 {
		return segs[0]
	}
	return fmt.Sprintf("%-6s %s", segs[0], segs[1])

}

func registers(registers, prevRegisters api.Registers) string {
	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true).Render("Registers") + "\n")
	for i, reg := range registers {
		style := lipgloss.NewStyle()
		if len(prevRegisters) != 0 && prevRegisters[i].Name == reg.Name && prevRegisters[i].Value != reg.Value {
			style = style.Foreground(lipgloss.Color("4")).Bold(true)
		}

		s := fmt.Sprintf("%3s %s", reg.Name, strings.ToUpper(reg.Value)[2:])
		sb.WriteString(style.Render(s))
		sb.WriteByte('\n')
	}

	return sb.String()
}

var dummytext = map[string]string{
	"registers": `Registers
PC    0000000102E38B70
SP    000000016D52B5B0
X0    0000000000000000
X1    0000000000000000
X2    0000000000000000
X3    0000000000000000
X4    0000000000000000
X5    0000000000000000
X6    0000000000000000
X7    0000000000000000
X8    0000000000000000
X9    0000000000000000
X10   0000000000000000`,
	"disass": `102A0B320    MOVD  16(R28), R16
102A0B324    SUB   $208, RSP, R17
102A0B328    CMP   R16, R17
102A0B32C    BLS   121(PC)
102A0B330*   SUB   $336, RSP, R20
102A0B334    STP   (R29, R30), -8(R20)
102A0B338    MOVD  R20, RSP
102A0B33C    SUB   $8, RSP, R29
102A0B340    MOVD  ZR, 120(RSP)
102A0B344    STP   (ZR, ZR), 104(RSP)
102A0B348    ADRP  8192(PC), R0
102A0B34C    ADD   $584, R0, R0
102A0B350    ORR   $7, ZR, R1
102A0B354    ADRP  4096(PC), R2
102A0B358    ADD   $3050, R2, R2
102A0B35C    MOVD  $5, R3
102A0B360    CALL  github.com/charmbracelet/bubbletea.LogToFile(SB)
102A0B364    MOVD  R0, 120(RSP)
102A0B368    MOVD  R1, 104(RSP)
102A0B36C    MOVD  R2, 112(RSP)
102A0B370    MOVD  120(RSP), R4
102A0B374    MOVD  R4, 184(RSP)
102A0B378    MOVD  104(RSP), R4
102A0B37C    MOVD  112(RSP), R5
102A0B380    MOVD  R4, 160(RSP)
102A0B384    MOVD  R5, 168(RSP)
102A0B388    MOVD  184(RSP), R4
102A0B38C    MOVD  R4, 64(RSP)
102A0B390    MOVD  168(RSP), R4
102A0B394    MOVD  160(RSP), R5
102A0B398    MOVD  R5, 88(RSP)
102A0B39C    MOVD  R4, 96(RSP)
102A0B3A0    CBNZ  R5, 78(PC)
102A0B3A4    JMP   1(PC)
102A0B3A8    STP   (ZR, ZR), 304(RSP)
102A0B3AC    MOVD  ZR, 320(RSP)
102A0B3B0    MOVD  64(RSP), R0`,
	"stack": `Stack
102A0B328    3F 02 10 EB 29 0F 00 54 3F 02 10 EB 29 0F 00 54 ................
102A0B330    F4 43 05 D1 9D FA 3F A9 3F 02 10 EB 29 0F 00 54 ................
102A0B338    9F 02 00 91 FD 23 00 D1 3F 02 10 EB 29 0F 00 54 ................
102A0B340    FF 3F 00 F9 FF FF 06 A9 3F 02 10 EB 29 0F 00 54 ................
102A0B348    00 00 00 D0 00 20 09 91 3F 02 10 EB 29 0F 00 54 ................
102A0B350    E1 0B 40 B2 02 00 00 B0 3F 02 10 EB 29 0F 00 54 ................
102A0B358    42 A8 2F 91 A3 00 80 D2 3F 02 10 EB 29 0F 00 54 ................
102A0B360    64 93 FF 97 E0 3F 00 F9 3F 02 10 EB 29 0F 00 54 ................
102A0B368    E1 37 00 F9 E2 3B 00 F9 3F 02 10 EB 29 0F 00 54 ................
102A0B370    E4 3F 40 F9 E4 5F 00 F9 3F 02 10 EB 29 0F 00 54 ................
102A0B378    E4 37 40 F9 E5 3B 40 F9 3F 02 10 EB 29 0F 00 54 ................
102A0B380    E4 53 00 F9 E5 57 00 F9 3F 02 10 EB 29 0F 00 54 ................
102A0B388    E5 2F 00 F9 E4 33 00 F9 3F 02 10 EB 29 0F 00 54 ................`,
	"source": `func main() {
	logfile, err := tea.LogToFile("tea.log", "debug")
	if err != nil {
		panic(err)
	}

	p := tea.NewProgram(model{logfile: logfile}, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		panic(err)
	}
}

type model struct {
	logfile       *os.File
	width, height int
}`,
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(debuglog, err)
		panic(err)
	}
}

func dump(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	os.Stdout.Write(b)
}
