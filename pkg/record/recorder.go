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
	"github.com/creack/termios/raw"
	"github.com/ohshell/cli/pkg/api"
	"github.com/sirupsen/logrus"
	"golang.org/x/term"
)

type Command struct {
	Timestamp time.Time
	Input     string
	Output    string
	Comment   string // parsed from bash comments
	Redacted  bool
}

type Session struct {
	Commands      []Command
	mu            sync.Mutex
	SlackThreadTS string
}

// SessionOption is a functional option for configuring a session.
type SessionOption func(*sessionConfig)

type sessionConfig struct {
	slackAudit    bool
	slackChannel  string
	token         string
	slackThreadTS string
}

// WithSlackAudit enables Slack audit logging for the session.
func WithSlackAudit(channel, token string) SessionOption {
	return func(cfg *sessionConfig) {
		cfg.slackAudit = true
		cfg.slackChannel = channel
		cfg.token = token
		ts, err := api.StartSlackAuditThread(channel, token)
		if err != nil {
			logrus.WithError(err).Error("Failed to start Slack audit thread")
			// decide if we should fail hard or just log
		} else {
			cfg.slackThreadTS = ts
		}
	}
}

// StdinInterceptor now takes a config for side effects
type StdinInterceptor struct {
	reader  io.Reader
	session *Session
	cmdCh   chan string
	closed  chan struct{}
	cfg     *sessionConfig
	lineBuf []byte // buffer for manual line buffering in raw mode
}

func (s *StdinInterceptor) Read(p []byte) (int, error) {
	logrus.Debug("StdinInterceptor.Read called")
	n, err := s.reader.Read(p)
	if n > 0 {
		// Robust line buffering: handle backspace and only append printable characters
		for i := 0; i < n; i++ {
			b := p[i]
			if b == 0x7f || b == 0x08 { // DEL or BS
				if len(s.lineBuf) > 0 {
					s.lineBuf = s.lineBuf[:len(s.lineBuf)-1]
				}
				continue
			}
			// Accept all bytes except NUL (0x00)
			if b != 0x00 {
				s.lineBuf = append(s.lineBuf, b)
			}
		}
		for {
			idxN := bytes.IndexByte(s.lineBuf, '\n')
			idxR := bytes.IndexByte(s.lineBuf, '\r')
			var idx int
			if idxN == -1 && idxR == -1 {
				break // no complete line yet
			}
			if idxN == -1 {
				idx = idxR
			} else if idxR == -1 {
				idx = idxN
			} else if idxN < idxR {
				idx = idxN
			} else {
				idx = idxR
			}
			line := s.lineBuf[:idx]
			s.lineBuf = s.lineBuf[idx+1:]
			trimmed := strings.TrimSpace(string(line))
			if trimmed != "" {
				select {
				case <-s.closed:
					return n, io.EOF
				case s.cmdCh <- trimmed:
					// Channel was successfully sent to
				default:
					// Channel is full or closed, skip this command
					logrus.Debug("cmdCh is full or closed, skipping command")
				}
				s.session.mu.Lock()
				s.session.Commands = append(s.session.Commands, Command{
					Timestamp: time.Now(),
					Input:     trimmed,
				})
				s.session.mu.Unlock()
				// Slack audit side effect
				if s.cfg != nil && s.cfg.slackAudit {
					go api.SendSlackAudit(trimmed, s.cfg.slackChannel, s.cfg.token, s.cfg.slackThreadTS)
				}
			}
		}
	}
	// If EOF and buffer has data, flush as a command
	if err == io.EOF && len(s.lineBuf) > 0 {
		trimmed := strings.TrimSpace(string(s.lineBuf))
		if trimmed != "" {
			select {
			case <-s.closed:
				return n, io.EOF
			case s.cmdCh <- trimmed:
				// Channel was successfully sent to
			default:
				// Channel is full or closed, skip this command
				logrus.Debug("cmdCh is full or closed, skipping command")
			}
			s.session.mu.Lock()
			s.session.Commands = append(s.session.Commands, Command{
				Timestamp: time.Now(),
				Input:     trimmed,
			})
			s.session.mu.Unlock()
			// Slack audit side effect
			if s.cfg != nil && s.cfg.slackAudit {
				go api.SendSlackAudit(trimmed, s.cfg.slackChannel, s.cfg.token, s.cfg.slackThreadTS)
			}
		}
		s.lineBuf = nil // clear buffer
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

// StartSession records a shell session. Options can enable features like Slack audit.
func StartSession(opts ...SessionOption) *Session {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}

	fmt.Fprintln(os.Stderr, "[ohsh notice] Commands entered using shell history (up arrow, CTRL-R, etc.) may not be captured correctly. Only directly typed commands are reliably recorded.\n\r")

	if os.Getenv("ZELLIJ") != "" || os.Getenv("TMUX") != "" || os.Getenv("STY") != "" {
		fmt.Fprintln(os.Stderr, "[archivist] Warning: Running inside a terminal multiplexer (zellij, tmux, or screen). Command tracking may not work correctly.")
	}

	fd := os.Stdin.Fd()
	if term.IsTerminal(int(fd)) {
		oldState, err := raw.MakeRaw(os.Stdin.Fd())
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to set terminal to raw mode: %v\n", err)
			return &Session{}
		}
		defer raw.TcSetAttr(fd, oldState)
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
	defer func() {
		logrus.Debug("Closing PTY...")
		_ = ptmx.Close()
	}()

	logrus.Debugf("Shell PID: %d", cmd.Process.Pid)
	fmt.Fprintf(os.Stdout, "🎥 Recording started: %s\n\r", shell)
	fmt.Fprintf(os.Stdout, "Press Ctrl+D when done to save and exit\n")

	cmdCh := make(chan string, 1)
	done := make(chan struct{})

	// Apply options to a config
	cfg := &sessionConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	session.SlackThreadTS = cfg.slackThreadTS

	// Setup stdin interceptor
	interceptor := &StdinInterceptor{
		reader:  os.Stdin,
		session: session,
		cmdCh:   cmdCh,
		closed:  done,
		cfg:     cfg,
	}

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
			case _, ok := <-cmdCh:
				if !ok {
					logrus.Debug("Output logger: cmdCh closed, flushing and exiting")
					if currentCmdIdx >= 0 {
						session.mu.Lock()
						session.Commands[currentCmdIdx].Output = outputBuf.String()
						session.mu.Unlock()
					}
					lastCmdIdxMu.Lock()
					lastCmdIdx = currentCmdIdx
					lastCmdIdxMu.Unlock()
					return
				}
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
			logrus.Debug("Closing cmdCh (input proxy)")
			wg.Done()
			close(cmdCh) // signal no more commands
		}()
		_, _ = io.Copy(ptmx, ctxReader)
		logrus.Debug("Input proxy goroutine finished")
	}()

	logrus.Debug("Waiting for shell process to exit...")
	err = cmd.Wait()
	logrus.Debugf("Shell process exited with err: %v", err)
	logrus.Debug("Closing PTY and cancelling context after shell exit")
	_ = ptmx.Close()
	cancel() // cancel context to unblock input proxy
	close(done)
	logrus.Debug("Waiting for goroutines to finish...")
	wg.Wait()

	lastCmdIdxMu.Lock()
	if lastCmdIdx >= 0 && lastCmdIdx < len(session.Commands) {
		// No extra output to flush, but this is where you'd do it if needed
	}
	lastCmdIdxMu.Unlock()

	fmt.Fprintf(os.Stdout, "🛑 Recording ended.\n\r")

	return session
}
