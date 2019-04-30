package drivers

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/hashicorp/nomad/plugins/drivers/proto"
)

// StreamToExecOptions is a convenience method to convert exec stream into
// ExecOptions object.
func StreamToExecOptions(
	ctx context.Context,
	command []string,
	tty bool,
	stream ExecTaskStream) (*ExecOptions, <-chan error) {

	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	errReader, errWriter := io.Pipe()
	resize := make(chan TerminalSize, 2)

	errCh := make(chan error, 1)

	// handle input
	go func() {
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				return
			}

			if msg.Stdin != nil && !msg.Stdin.Close {
				_, err := inWriter.Write(msg.Stdin.Data)
				if err != nil {
					errCh <- err
					return
				}
			} else if msg.Stdin != nil && msg.Stdin.Close {
				inWriter.Close()
			} else if msg.TtySize != nil {
				resize <- TerminalSize{
					Height: int(msg.TtySize.Height),
					Width:  int(msg.TtySize.Width),
				}
			} else if isHeartbeat(msg) {
				// do nothing
			} else {
				errCh <- fmt.Errorf("unexpected message type: %#v", msg)
			}
		}
	}()

	var sendLock sync.Mutex
	send := func(v *ExecTaskStreamingResponseMsg) error {
		sendLock.Lock()
		defer sendLock.Unlock()

		return stream.Send(v)
	}

	var outWg sync.WaitGroup
	outWg.Add(2)
	// handle Stdout
	go func() {
		defer outWg.Done()

		reader := outReader
		bytes := make([]byte, 1024)
		msg := &ExecTaskStreamingResponseMsg{Stdout: &proto.ExecTaskStreamingOperation{}}

		for {
			n, err := reader.Read(bytes)
			if err == io.EOF || err == io.ErrClosedPipe {
				msg.Stdout.Data = nil
				msg.Stdout.Close = true
				send(msg)
				break
			}
			if err != nil {
				errCh <- err
				break
			}

			msg.Stdout.Data = bytes[:n]
			send(msg)
		}

	}()
	// handle Stderr
	go func() {
		defer outWg.Done()

		reader := errReader
		bytes := make([]byte, 1024)
		msg := &ExecTaskStreamingResponseMsg{Stderr: &proto.ExecTaskStreamingOperation{}}

		for {
			n, err := reader.Read(bytes)
			if err == io.EOF || err == io.ErrClosedPipe {
				msg.Stderr.Data = nil
				msg.Stderr.Close = true
				send(msg)
				break
			}
			if err != nil {
				errCh <- err
				break
			}

			msg.Stderr.Data = bytes[:n]
			send(msg)
		}

	}()

	doneCh := make(chan error, 1)
	go func() {
		outWg.Wait()

		select {
		case err := <-errCh:
			doneCh <- err
		default:
			doneCh <- nil
		}
		close(doneCh)
	}()

	return &ExecOptions{
		Command: command,
		Tty:     tty,

		Stdin:  inReader,
		Stdout: outWriter,
		Stderr: errWriter,

		ResizeCh: resize,
	}, doneCh
}

func NewExecStreamingResponseExit(exitCode int) *ExecTaskStreamingResponseMsg {
	return &ExecTaskStreamingResponseMsg{
		Exited: true,
		Result: &proto.ExitResult{
			ExitCode: int32(exitCode),
		},
	}

}

func isHeartbeat(r *ExecTaskStreamingRequestMsg) bool {
	return r.Stdin == nil && r.Setup == nil && r.TtySize == nil
}
