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
						fmt.Fprintln(os.Stderr, "[archivist][debug] StdinInterceptor closed")
						return n, io.EOF
					case s.cmdCh <- trimmed:
						fmt.Fprintf(os.Stderr, "[archivist][debug] Command intercepted: %q\n", trimmed)
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

	fmt.Fprintf(os.Stderr, "[archivist][debug] Shell command: %s\n", shell)

	session := &Session{}
	cmd := exec.Command(shell)

	fmt.Fprintf(os.Stderr, "[archivist][debug] Starting shell process...\n")
	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start shell: %v\n", err)
		return session
	}
	fmt.Fprintf(os.Stderr, "[archivist][debug] Shell started. PTY fd: %d\n", ptmx.Fd())
	defer func() {
		fmt.Fprintf(os.Stderr, "[archivist][debug] Closing PTY...\n")
		_ = ptmx.Close()
	}()

	fmt.Fprintf(os.Stderr, "[archivist][debug] Shell PID: %d\n", cmd.Process.Pid)
	fmt.Printf("[archivist] Recording shell session: %s\n", shell)

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
		fmt.Fprintln(os.Stderr, "[archivist][debug] Output logger goroutine started")
		defer func() {
			wg.Done()
			fmt.Fprintln(os.Stderr, "[archivist][debug] Output logger goroutine exiting")
		}()
		var outputBuf bytes.Buffer
		currentCmdIdx := -1
		ptyReader := bufio.NewReader(ptmx)
		for {
			select {
			case <-done:
				fmt.Fprintln(os.Stderr, "[archivist][debug] Output logger received done signal")
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
				fmt.Fprintln(os.Stderr, "[archivist][debug] Output logger: new command detected")
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
						fmt.Fprintln(os.Stderr, "[archivist][debug] Output logger: ptyReader EOF")
					}
					fmt.Fprintf(os.Stderr, "[archivist][debug] Output logger: ptyReader error: %v\n", err)
					if currentCmdIdx >= 0 {
						session.mu.Lock()
						session.Commands[currentCmdIdx].Output = outputBuf.String()
						session.mu.Unlock()
					}
					lastCmdIdxMu.Lock()
					lastCmdIdx = currentCmdIdx
					lastCmdIdxMu.Unlock()
					fmt.Fprintln(os.Stderr, "[archivist][debug] Output logger: returning")
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
		fmt.Fprintln(os.Stderr, "[archivist][debug] Input proxy goroutine started")
		defer func() {
			fmt.Fprintln(os.Stderr, "[archivist][debug] Input proxy goroutine exiting")
			wg.Done()
		}()
		_, _ = io.Copy(ptmx, ctxReader)
		close(cmdCh) // signal no more commands
	}()

	fmt.Fprintf(os.Stderr, "[archivist][debug] Waiting for shell process to exit...\n")
	err = cmd.Wait()
	fmt.Fprintf(os.Stderr, "[archivist][debug] Shell process exited with err: %v\n", err)
	close(done)
	cancel() // cancel context to unblock input proxy
	fmt.Fprintln(os.Stderr, "[archivist][debug] Waiting for goroutines to finish...")
	wg.Wait()

	// After all goroutines finish, ensure last output is flushed
	lastCmdIdxMu.Lock()
	if lastCmdIdx >= 0 && lastCmdIdx < len(session.Commands) {
		// No extra output to flush, but this is where you'd do it if needed
	}
	lastCmdIdxMu.Unlock()

	fmt.Println("[archivist] Recording ended.")

	return session
}
