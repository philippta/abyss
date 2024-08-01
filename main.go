package main

import (
	"fmt"
	"os"
	"strings"

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
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		fmt.Fprintln(debuglog, msg.String())
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			m.dbg.StepInstruction(false)
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	}

	return m, nil
}

func (m model) View() string {
	asm := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		BorderBottom(true).
		Width(m.width / 2).
		Height(m.height - 15)

	paneRegisters := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Width(m.width / 2).
		Height(m.height - 15)

	mem := lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderRight(true).
		Width(m.width / 2).
		Height(15)

	return lipgloss.JoinVertical(
		lipgloss.Top,
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			asm.Render(disassembly(m.dbg)),
			paneRegisters.Render(registers(m.dbg)),
		),
		lipgloss.JoinHorizontal(
			lipgloss.Top,
			mem.Render(dummytext["stack"]),
		),
	)
}

func disassembly(c *rpc2.RPCClient) string {
	state, err := c.GetState()
	must(err)

	asms, err := c.DisassemblePC(api.EvalScope{GoroutineID: state.CurrentThread.GoroutineID}, state.CurrentThread.PC, api.IntelFlavour)
	must(err)

	var sb strings.Builder
	for _, asm := range asms {
		fmt.Fprintf(&sb, "%X %-30s\n", asm.Loc.PC, lipgloss.NewStyle().Bold(asm.AtPC).Render(asm.Text))
	}

	return sb.String()
}

func registers(c *rpc2.RPCClient) string {
	state, err := c.GetState()
	must(err)

	regs, err := c.ListScopeRegisters(api.EvalScope{GoroutineID: state.CurrentThread.GoroutineID}, false)
	must(err)

	var sb strings.Builder
	for _, reg := range regs {
		fmt.Fprintf(&sb, "%3s %s\n", reg.Name, strings.ToUpper(reg.Value)[2:])
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
