package main

import (
	"bufio"
	"github.com/hpcloud/tail"
	"github.com/jessevdk/go-flags"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"syscall"
	"time"
)

var logger *log.Logger

// Kelthuzad monitors a log or stdout, kills a sick one and respawns a normal one.
type Kelthuzad struct {
	cmd    *exec.Cmd
	opt    *opts
	regex  *regexp.Regexp
	stdout io.ReadCloser
}

// opts have several options for argument parsing.
type opts struct {
	LogPath string `short:"p" long:"path" description:"The path of the log"`
	CmdPath string `short:"c" long:"command" description:"The path of a command string to respawn the process" required:"true"`
	Regex   string `short:"r" long:"regex" description:"The regex pattern to detect a failure" required:"true"`
	Verbose bool   `short:"v" long:"verbose" description:"Print a verbose message to stdout"`
	Delay   int    `short:"d" long:"delay" description:"The seconds for waiting after respawning" default:"5"`
}

// New returns initialized Kelthuzad pointer
func New(opt *opts) *Kelthuzad {
	kel := &Kelthuzad{}
	kel.opt = opt
	kel.regex = regexp.MustCompile(kel.opt.Regex)
	kel.spawn()

	return kel
}

// spawn executes the command from k.opt.CmdPath and assigns it into k's cmd field.
func (k *Kelthuzad) spawn() {
	cmd := exec.Command(k.opt.CmdPath)

	if k.opt.LogPath == "" {
		// get the stdout pipe before it starts and assign it into k.stdout to monitor stdout
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			logger.Fatal(err)
		}
		k.stdout = stdout
	}

	// this block is necessary when killing a subprocess properly
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	go func() {
		err := cmd.Start()
		if err != nil {
			logger.Fatalln(err)
		}
		logger.Printf("%v is spawned\n", cmd.Process.Pid)
		cmd.Wait()
		logger.Printf("%v is done!\n", cmd.Process.Pid)
	}()

	// return the created Cmd struct
	k.cmd = cmd
}

// kill kills current k.cmd.
func (k *Kelthuzad) kill() {
	pgid, err := syscall.Getpgid(k.cmd.Process.Pid)
	if err == nil {
		syscall.Kill(-pgid, 15)
	} else {
		logger.Fatal(err)
	}
}

// check checks whether the line matches with the k.regex pattern.
func (k *Kelthuzad) check(line string) {
	// if the line contains the pattern of k.regex
	if k.regex.MatchString(line) {
		// notify it
		logger.Printf("[FAIL] %v -> %v\n", line, k.opt.Regex)

		// wait to avoid being with flooded with respawning
		logger.Printf("Waiting %v seconds...\n", k.opt.Delay)
		time.Sleep(time.Second * time.Duration(k.opt.Delay))

		// kill the sick one
		k.kill()

		// respawn the normal one
		k.spawn()

		// if the Verbose flag is set, also print normal lines
	} else if k.opt.Verbose {
		logger.Println(line)
	}
}

// monitorLog monitors the specific log with tail and checks any changes whenever log populated.
func (k *Kelthuzad) monitorLog() {
	// get the Tail struct for monitoring the last part of the log
	t, err := tail.TailFile(k.opt.LogPath, tail.Config{Follow: true, Location: &tail.SeekInfo{Offset: 0, Whence: os.SEEK_END}})
	if err != nil {
		logger.Fatalln(err)
	}

	// monitor the log
	for line := range t.Lines {
		k.check(line.Text)
	}
}

// monitorStdout monitors the stdout of the process and checks it.
func (k *Kelthuzad) monitorStdout() {
	for {
		scanner := bufio.NewScanner(k.stdout)
		for scanner.Scan() {
			k.check(scanner.Text())
		}
	}
}

// Monitor monitors appropriate one depending on LogPath option.
func (k *Kelthuzad) Monitor() {
	if k.opt.LogPath != "" {
		logger.Println("monitoring log...")
		k.monitorLog()
	} else {
		logger.Println("monitoring stdout...")
		k.monitorStdout()
	}
}

func main() {
	// initialize empty options
	opt := &opts{}

	// set the logger
	logger = log.New(os.Stdout, "", log.LstdFlags|log.Ltime)

	// parse the arguments
	_, err := flags.Parse(opt)
	if err != nil {
		os.Exit(1)
	}

	// get a kelthuzad object
	kel := New(opt)

	// handle an interrupt for terminate children process and itself gracefully
	signalChan := make(chan os.Signal)
	signal.Notify(signalChan, os.Interrupt)
	go func() {
		<-signalChan
		logger.Println("recieved an interrupt, stopping...\n")
		kel.kill()
		os.Exit(0)
	}()

	// start monitoring
	kel.Monitor()
}