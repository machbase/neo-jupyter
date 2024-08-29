package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	pid := flag.String("pid", "neo-jupyter.pid", "pid file")
	flag.Parse()

	python := findPython()
	if python == "" {
		fmt.Fprintln(os.Stderr, "python not found")
		os.Exit(1)
	}
	jupyter := findJupyterExecutable()
	if jupyter == "" {
		fmt.Fprintln(os.Stderr, "jupyter not found")
		os.Exit(1)
	}

	notebookDir := "."
	if dir := os.Getenv("MACHBASE_NEO_FILE"); dir != "" {
		toks := strings.Split(dir, string(filepath.ListSeparator))
		if len(toks) > 0 {
			notebookDir = toks[0]
		}
	}

	jl := &JupyterLash{
		pythonBin:   python,
		jupyterBin:  jupyter,
		notebookDir: notebookDir,
	}
	jl.Start()

	os.WriteFile(*pid, []byte(fmt.Sprintf("%d", os.Getpid())), 0644)

	// wait Ctrl+C
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)
	fmt.Println("started, press ctrl+c to stop...")
	<-done

	jl.Stop()
}

type JupyterLash struct {
	sync.RWMutex
	pythonBin   string
	jupyterBin  string
	notebookDir string
	cmd         *exec.Cmd
}

func (jl *JupyterLash) Start() {
	jl.Lock()
	defer jl.Unlock()
	if jl.cmd != nil {
		return
	}
	jl.start0()
}

func (jl *JupyterLash) Stop() {
	jl.Lock()
	defer jl.Unlock()
	if jl.cmd == nil {
		return
	}
	jl.stop0()
}

func (jl *JupyterLash) start0() {
	// --ip=<ip> --port=<int>
	cmd := exec.Command(jl.pythonBin, jl.jupyterBin, "lab", "-y", "--no-browser", "--notebook-dir", jl.notebookDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = []string{}
	if home, err := os.UserHomeDir(); err == nil {
		cmd.Env = append(cmd.Env, fmt.Sprintf("HOME=%s", home))
	}
	startWg := sync.WaitGroup{}
	startWg.Add(1)
	go func() {
		err := cmd.Start()
		if err != nil {
			jl.cmd = nil
			jl.logError("fail to start: cmd:%q error:%v", jl.jupyterBin, err)
			startWg.Done()
			return
		} else {
			startWg.Done()
		}
		err = cmd.Wait()
		if err != nil {
			jl.logError("fail to run: %v", err)
		} else {
			if jl.cmd != nil && jl.cmd.Process != nil {
				jl.log("exit code", jl.cmd.ProcessState.ExitCode())
			}
		}
		jl.cmd = nil
	}()
	startWg.Wait()
}

func (jl *JupyterLash) stop0() {
	if jl.cmd == nil || jl.cmd.Process == nil {
		return
	}
	cmd := exec.Command(jl.jupyterBin, "server", "stop")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	err := cmd.Start()
	if err != nil {
		jl.logError("fail to stop: cmd:%q error:%v", jl.jupyterBin, err)
		return
	}
	err = cmd.Wait()
	if err != nil {
		jl.logError("fail to run: %v", err)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		count := 0
		dur := 100 * time.Millisecond
		tick := time.NewTimer(dur)
		for range tick.C {
			if jl.cmd == nil {
				break
			}
			count++
			if time.Duration(count)*dur > 5*time.Second {
				jl.logError("timeout")
				break
			}
		}
	}()
	wg.Wait()
}

func findPython() string {
	list := []string{
		"/usr/bin/python3",
		"/usr/bin/python",
	}

	for _, path := range list {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func findJupyterExecutable() string {
	list := []string{
		"$HOME/.local/bin/jupyter",
		"/usr/local/bin/jupyter",
	}

	for _, path := range list {
		if strings.Contains(path, "$HOME") {
			if home, err := os.UserHomeDir(); err == nil {
				path = strings.ReplaceAll(path, "$HOME", home)
			}
		}
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func (jl *JupyterLash) log(f string, args ...any) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stdout, f)
	} else {
		fmt.Fprintf(os.Stdout, f+"\n", args...)
	}
}

func (jl *JupyterLash) logError(f string, args ...any) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, f)
	} else {
		fmt.Fprintf(os.Stderr, f+"\n", args...)
	}
}
