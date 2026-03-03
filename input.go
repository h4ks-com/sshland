package main

import (
	"io"
	"os"
	"sync"
)

// sshInputMux reads from an SSH session in a single goroutine and buffers data
// into a channel. This prevents the "two goroutines racing for one byte" bug:
// when BubbleTea exits and its fallback cancel-reader goroutine is stuck inside
// sess.Read(), the next BubbleTea iteration starts a second goroutine, both race
// to read from the same channel, and the stale one wins and discards the byte.
//
// By funnelling everything through one goroutine here, there is never more than
// one concurrent reader of the underlying SSH session.
type sshInputMux struct {
	ch chan []byte
}

func newSSHInputMux(r io.Reader) *sshInputMux {
	m := &sshInputMux{ch: make(chan []byte, 64)}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				m.ch <- chunk
			}
			if err != nil {
				close(m.ch)
				return
			}
		}
	}()
	return m
}

// sshPipe pumps data from an sshInputMux into an os.Pipe. Giving BubbleTea a
// real os.File (the read end) means cancelreader.NewReader uses kqueue/epoll,
// so Cancel() returns immediately without goroutine leaks or lost bytes.
type sshPipe struct {
	mux  *sshInputMux
	pr   *os.File
	pw   *os.File
	done chan struct{}
	wg   sync.WaitGroup
}

func newSSHPipe(mux *sshInputMux) (*sshPipe, error) {
	pr, pw, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	p := &sshPipe{
		mux:  mux,
		pr:   pr,
		pw:   pw,
		done: make(chan struct{}),
	}
	p.wg.Add(1)
	go p.pump()
	return p, nil
}

func (p *sshPipe) pump() {
	defer func() {
		_ = p.pw.Close()
		p.wg.Done()
	}()
	for {
		select {
		case <-p.done:
			return
		case chunk, ok := <-p.mux.ch:
			if !ok {
				return
			}
			if _, err := p.pw.Write(chunk); err != nil {
				return
			}
		}
	}
}

// stop signals the pump goroutine, waits for it to finish (so no more writes
// reach pw), then closes both pipe ends.
func (p *sshPipe) stop() {
	close(p.done)
	p.wg.Wait()
	_ = p.pr.Close()
}

// sshPipeFile wraps sshPipe so that it satisfies cancelreader.File.
// cancelreader.NewReader detects the Fd() method and uses kqueue/epoll
// instead of the fallback goroutine reader.
type sshPipeFile struct {
	*sshPipe
}

func (f *sshPipeFile) Read(b []byte) (int, error)  { return f.pr.Read(b) }
func (f *sshPipeFile) Write(b []byte) (int, error) { return 0, io.ErrClosedPipe }
func (f *sshPipeFile) Close() error                { return f.pr.Close() }
func (f *sshPipeFile) Fd() uintptr                 { return f.pr.Fd() }
func (f *sshPipeFile) Name() string                { return f.pr.Name() }
