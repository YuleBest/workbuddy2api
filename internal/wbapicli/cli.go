package wbapicli

import (
	"fmt"
	"strconv"
	"strings"
)

func Run(opts Options) int {
	app := newApp(opts)
	if len(opts.Args) > 0 && opts.Args[0] == "healthcheck" {
		app.quietConfig = true
	}
	app.loadConfig()
	return app.run(opts.Args)
}

func (a *App) run(args []string) int {
	cmd := "status"
	var rest []string
	if len(args) > 0 {
		cmd = args[0]
		rest = args[1:]
	}
	switch cmd {
	case "status", "check":
		return a.cmdStatus()
	case "accounts":
		return a.cmdAccounts()
	case "quota", "pool":
		return a.cmdQuota(rest)
	case "acct", "account":
		return a.cmdAcct(rest)
	case "task":
		return a.cmdTask(rest)
	case "models", "list":
		return a.cmdModels(rest)
	case "stats":
		return a.cmdStats(rest)
	case "panel":
		return a.cmdPanel()
	case "config":
		return a.cmdConfig()
	case "doctor", "check-up":
		return a.cmdDoctor()
	case "key":
		return a.cmdKey(args, rest)
	case "log":
		n := 30
		if len(rest) > 0 {
			value, err := strconv.Atoi(rest[0])
			if err != nil {
				return a.fail(fmt.Sprintf("日志行数必须是整数：%s", rest[0]))
			}
			n = value
		}
		return a.cmdLog(n)
	case "start", "stop", "restart":
		return a.cmdService(cmd)
	case "tunnel":
		return a.cmdTunnel(rest)
	case "healthcheck":
		return a.cmdHealthcheck()
	case "help", "-h", "--help":
		fmt.Fprint(a.stdout, helpText)
		return 0
	default:
		fmt.Fprint(a.stdout, helpText)
		return 2
	}
}

func (a *App) cmdKey(allArgs, rest []string) int {
	sub := "list"
	if len(rest) > 0 {
		sub = rest[0]
	}
	switch sub {
	case "list":
		show := false
		for _, arg := range allArgs {
			if arg == "--show" {
				show = true
				break
			}
		}
		return a.cmdKeyList(show)
	case "new":
		note := ""
		if len(rest) > 1 {
			note = rest[1]
		}
		return a.cmdKeyNew(note)
	case "revoke", "rm", "delete":
		if len(rest) < 2 {
			return a.fail("用法: wbapi key revoke <前缀>")
		}
		return a.cmdKeyRevoke(rest[1])
	default:
		fmt.Fprint(a.stdout, helpText)
		return 0
	}
}

func pythonRepr(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", `\'`) + "'"
}
