package main

import (
	"fmt"
	"os"
	"transport-monitoring/internal/kt04"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: kt04 setup|init|security|backup|replication|analytics|failures|lag|pitr|package")
		os.Exit(1)
	}
	root, e := os.Getwd()
	if e != nil {
		panic(e)
	}
	r := kt04.New(root)
	switch os.Args[1] {
	case "setup":
		e = kt04.Setup(root)
	case "init":
		e = r.Init()
	case "security":
		e = r.Security()
	case "backup":
		e = r.Backup()
	case "replication":
		e = r.Replication()
	case "failures":
		e = r.Failures()
	case "lag":
		e = r.Lag()
	case "analytics":
		e = r.Analytics()
	case "pitr":
		e = r.PITR()
	case "package":
		e = r.Package()
	default:
		e = fmt.Errorf("unknown stage")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
