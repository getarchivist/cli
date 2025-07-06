package record

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/sirupsen/logrus"
)

type Command struct {
	Timestamp time.Time
	Input     string
	Output    string
	Comment   string // parsed from bash comments
	Redacted  bool
}

type Session struct {
	Commands []Command
	mu       sync.Mutex
}

// StdinInterceptor wraps an io.Reader and logs each line entered
// to the provided session.
type StdinInterceptor struct {
	reader  io.Reader
	session *Session
	cmdCh   chan string
	closed  chan struct{}
}

func (s *StdinInterceptor) Read(p []byte) (int, error) {
	n, err := s.reader.Read(p)
	if n > 0 {
		// Split input into lines (commands)
		lines := strings.Split(string(p[:n]), "\n")
		for i, line := range lines {
			if i < len(lines)-1 || (err == io.EOF && line != "") {
				// Log the command (ignore empty lines)
				trimmed := strings.TrimSpace(line)
				if trimmed != "" {
					select {
					case <-s.closed:
						logrus.Debug("StdinInterceptor closed")
						return n, io.EOF
					case s.cmdCh <- trimmed:
						logrus.Debugf("Command intercepted: %q", trimmed)
					}
					s.session.mu.Lock()
					s.session.Commands = append(s.session.Commands, Command{
						Timestamp: time.Now(),
						Input:     trimmed,
					})
					s.session.mu.Unlock()
				}
			}
		}
	}
	return n, err
}

// ContextReader wraps an io.Reader and a context.Context, returning on context cancellation.
type ContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *ContextReader) Read(p []byte) (int, error) {
	readCh := make(chan struct {
		n   int
		err error
	}, 1)
	go func() {
		n, err := cr.r.Read(p)
		readCh <- struct {
			n   int
			err error
		}{n, err}
	}()
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	case res := <-readCh:
		return res.n, res.err
	}
}

func StartSession() *Session {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	// Warn if running inside a multiplexer
	if os.Getenv("ZELLIJ") != "" || os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		fmt.Fprintln(os.Stderr, "[archivist] Warning: Running inside a terminal multiplexer (zellij, tmux, or screen). Command tracking may not work correctly.")
	}

	logrus.Debugf("Shell command: %s", shell)

	session := &Session{}
	cmd := exec.Command(shell)

	logrus.Debug("Starting shell process...")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start shell: %v\n", err)
		return session
	}
	logrus.Debugf("Shell started. PTY fd: %d", ptmx.Fd())
	defer func() {
		logrus.Debug("Closing PTY...")
		_ = ptmx.Close()
	}()

	logrus.Debugf("Shell PID: %d", cmd.Process.Pid)
	fmt.Printf("🎥 Recording started: %s\n", shell)
	fmt.Println("Press Ctrl+D when done to save and exit")

	cmdCh := make(chan string, 1)
	done := make(chan struct{})
	interceptor := &StdinInterceptor{reader: os.Stdin, session: session, cmdCh: cmdCh, closed: done}

	var wg sync.WaitGroup
	wg.Add(2)

	var lastCmdIdxMu sync.Mutex
	lastCmdIdx := -1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctxReader := &ContextReader{ctx: ctx, r: interceptor}

	// Output logger goroutine
	go func() {
		logrus.Debug("Output logger goroutine started")
		defer func() {
			wg.Done()
			logrus.Debug("Output logger goroutine exiting")
		}()
		var outputBuf bytes.Buffer
		currentCmdIdx := -1
		ptyReader := bufio.NewReader(ptmx)
		for {
			select {
			case <-done:
				logrus.Debug("Output logger received done signal")
				if currentCmdIdx >= 0 {
					session.mu.Lock()
					session.Commands[currentCmdIdx].Output = outputBuf.String()
					session.mu.Unlock()
				}
				lastCmdIdxMu.Lock()
				lastCmdIdx = currentCmdIdx
				lastCmdIdxMu.Unlock()
				return
			case <-cmdCh:
				logrus.Debug("Output logger: new command detected")
				if currentCmdIdx >= 0 {
					session.mu.Lock()
					session.Commands[currentCmdIdx].Output = outputBuf.String()
					session.mu.Unlock()
					outputBuf.Reset()
				}
				currentCmdIdx++
				lastCmdIdxMu.Lock()
				lastCmdIdx = currentCmdIdx
				lastCmdIdxMu.Unlock()
			default:
				b, err := ptyReader.ReadByte()
				if err != nil {
					if err == io.EOF {
						logrus.Debug("Output logger: ptyReader EOF")
					}
					logrus.Debugf("Output logger: ptyReader error: %v", err)
					if currentCmdIdx >= 0 {
						session.mu.Lock()
						session.Commands[currentCmdIdx].Output = outputBuf.String()
						session.mu.Unlock()
					}
					lastCmdIdxMu.Lock()
					lastCmdIdx = currentCmdIdx
					lastCmdIdxMu.Unlock()
					logrus.Debug("Output logger: returning")
					return
				}
				if currentCmdIdx >= 0 {
					outputBuf.WriteByte(b)
				}
				os.Stdout.Write([]byte{b})
			}
		}
	}()

	// Input proxy goroutine
	go func() {
		logrus.Debug("Input proxy goroutine started")
		defer func() {
			logrus.Debug("Input proxy goroutine exiting")
			wg.Done()
		}()
		_, _ = io.Copy(ptmx, ctxReader)
		close(cmdCh) // signal no more commands
	}()

	logrus.Debug("Waiting for shell process to exit...")
	err = cmd.Wait()
	logrus.Debugf("Shell process exited with err: %v", err)
	close(done)
	cancel() // cancel context to unblock input proxy
	logrus.Debug("Waiting for goroutines to finish...")
	wg.Wait()

	// After all goroutines finish, ensure last output is flushed
	lastCmdIdxMu.Lock()
	if lastCmdIdx >= 0 && lastCmdIdx < len(session.Commands) {
		// No extra output to flush, but this is where you'd do it if needed
	}
	lastCmdIdxMu.Unlock()

	fmt.Println("🛑 Recording ended.")

	return session
}
