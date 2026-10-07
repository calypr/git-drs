package filter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"sync"
	"testing"

	"github.com/git-lfs/pktline"
)

func TestGitFilterStreamsLargePayloadWithBoundedHeap(t *testing.T) {
	const payloadSize = 32 << 20
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	t.Cleanup(func() {
		_ = inputReader.Close()
		_ = inputWriter.Close()
		_ = outputReader.Close()
		_ = outputWriter.Close()
	})

	copyComplete := make(chan error, 1)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseHandler := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseHandler()
	filter := NewGitFilter(inputReader, outputWriter, slog.New(slog.NewTextHandler(io.Discard, nil))).OnClean(
		func(_ context.Context, _ FilterRequest, content io.Reader, dst io.Writer) error {
			_, err := io.Copy(dst, content)
			copyComplete <- err
			<-release
			return err
		},
	)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	filterErr := make(chan error, 1)
	go func() {
		err := filter.Run(context.Background())
		_ = outputWriter.Close()
		filterErr <- err
	}()
	inputErr := make(chan error, 1)
	go func() {
		err := writeLargeFilterRequest(inputWriter, payloadSize)
		if err != nil {
			_ = inputWriter.CloseWithError(err)
			inputErr <- err
			return
		}
		inputErr <- inputWriter.Close()
	}()

	response := pktline.NewPktline(outputReader, nil)
	serverInit, err := response.ReadPacketList()
	if err != nil {
		t.Fatalf("read server init: %v", err)
	}
	if !reflect.DeepEqual(serverInit, []string{"git-filter-server", "version=2"}) {
		t.Fatalf("unexpected server init list: %v", serverInit)
	}
	capabilities, err := response.ReadPacketList()
	if err != nil {
		t.Fatalf("read server capabilities: %v", err)
	}
	if !reflect.DeepEqual(capabilities, []string{"capability=clean", "capability=smudge"}) {
		t.Fatalf("unexpected capabilities: %v", capabilities)
	}
	if err := <-copyComplete; err != nil {
		t.Fatalf("copy request content: %v", err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	retained := uint64(0)
	if after.HeapAlloc > before.HeapAlloc {
		retained = after.HeapAlloc - before.HeapAlloc
	}
	if retained >= payloadSize/2 {
		t.Fatalf("retained %d heap bytes after copying %d-byte request, want less than %d", retained, payloadSize, payloadSize/2)
	}
	releaseHandler()
	status, err := response.ReadPacketList()
	if err != nil {
		t.Fatalf("read response status: %v", err)
	}
	if !reflect.DeepEqual(status, []string{"status=success"}) {
		t.Fatalf("unexpected response status: %v", status)
	}

	got := &countingHashWriter{hash: sha256.New()}
	if _, err := io.Copy(got, pktline.NewPktlineReaderFromPktline(response, pktline.MaxPacketLength)); err != nil {
		t.Fatalf("read response content: %v", err)
	}
	if got.count != payloadSize {
		t.Fatalf("response size = %d, want %d", got.count, payloadSize)
	}
	wantHash := sha256.New()
	chunk := bytes.Repeat([]byte{'x'}, 32*1024)
	for remaining := payloadSize; remaining > 0; {
		n := len(chunk)
		if n > remaining {
			n = remaining
		}
		_, _ = wantHash.Write(chunk[:n])
		remaining -= n
	}
	if !bytes.Equal(got.hash.Sum(nil), wantHash.Sum(nil)) {
		t.Fatal("response payload checksum differs from request")
	}
	terminator, err := response.ReadPacketList()
	if err != nil {
		t.Fatalf("read response terminator: %v", err)
	}
	if len(terminator) != 0 {
		t.Fatalf("response terminator = %v, want empty list", terminator)
	}

	if err := <-inputErr; err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := <-filterErr; err != nil {
		t.Fatalf("run filter: %v", err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("streamed %d-byte request and response with %d allocated bytes and %d retained bytes after copying", payloadSize, allocated, retained)
}

func TestGitFilterDrainsFailedRequestBeforeNextRequest(t *testing.T) {
	var input bytes.Buffer
	protocol := pktline.NewPktline(nil, &input)
	for _, packet := range []func() error{
		func() error { return protocol.WritePacketText("git-filter-client") },
		func() error { return protocol.WritePacketList([]string{"version=2"}) },
		func() error { return protocol.WritePacketList([]string{"capability=clean", "capability=smudge"}) },
	} {
		if err := packet(); err != nil {
			t.Fatalf("write handshake: %v", err)
		}
	}
	if err := writeFilterRequest(&input, "clean", "first.bin", "first request payload"); err != nil {
		t.Fatalf("write first request: %v", err)
	}
	if err := writeFilterRequest(&input, "clean", "second.bin", "second request payload"); err != nil {
		t.Fatalf("write second request: %v", err)
	}

	var output bytes.Buffer
	f := NewGitFilter(&input, &output, slog.New(slog.NewTextHandler(io.Discard, nil))).OnClean(
		func(_ context.Context, req FilterRequest, content io.Reader, dst io.Writer) error {
			if req.Pathname == "first.bin" {
				if _, err := io.WriteString(dst, "must-not-leak"); err != nil {
					return err
				}
				return errors.New("intentional handler failure")
			}
			_, err := io.Copy(dst, content)
			return err
		},
	)
	if err := f.Run(context.Background()); err != nil {
		t.Fatalf("run filter: %v", err)
	}

	response := pktline.NewPktline(&output, nil)
	for _, want := range [][]string{
		{"git-filter-server", "version=2"},
		{"capability=clean", "capability=smudge"},
		{"status=error"},
		{"status=success"},
	} {
		got, err := response.ReadPacketList()
		if err != nil {
			t.Fatalf("read response list: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("response list = %v, want %v", got, want)
		}
		if reflect.DeepEqual(want, []string{"status=success"}) {
			payload, err := io.ReadAll(pktline.NewPktlineReaderFromPktline(response, pktline.MaxPacketLength))
			if err != nil {
				t.Fatalf("read second response payload: %v", err)
			}
			if string(payload) != "second request payload" {
				t.Fatalf("second response payload = %q", payload)
			}
			terminator, err := response.ReadPacketList()
			if err != nil || len(terminator) != 0 {
				t.Fatalf("read second response terminator: list=%v err=%v", terminator, err)
			}
		}
	}
}

func writeLargeFilterRequest(dst io.Writer, payloadSize int) error {
	protocol := pktline.NewPktline(nil, dst)
	for _, packet := range []func() error{
		func() error { return protocol.WritePacketText("git-filter-client") },
		func() error { return protocol.WritePacketList([]string{"version=2"}) },
		func() error { return protocol.WritePacketList([]string{"capability=clean", "capability=smudge"}) },
		func() error { return protocol.WritePacketList([]string{"command=clean", "pathname=large.bin"}) },
	} {
		if err := packet(); err != nil {
			return err
		}
	}

	writer := pktline.NewPktlineWriter(dst, pktline.MaxPacketLength)
	chunk := bytes.Repeat([]byte{'x'}, 32*1024)
	for remaining := payloadSize; remaining > 0; {
		n := len(chunk)
		if n > remaining {
			n = remaining
		}
		written, err := writer.Write(chunk[:n])
		if err != nil {
			return err
		}
		remaining -= written
	}
	return writer.Flush()
}

func writeFilterRequest(dst io.Writer, command, pathname, payload string) error {
	protocol := pktline.NewPktline(nil, dst)
	if err := protocol.WritePacketList([]string{"command=" + command, "pathname=" + pathname}); err != nil {
		return err
	}
	writer := pktline.NewPktlineWriter(dst, pktline.MaxPacketLength)
	if _, err := io.WriteString(writer, payload); err != nil {
		return err
	}
	return writer.Flush()
}

type countingHashWriter struct {
	hash interface {
		Write([]byte) (int, error)
		Sum([]byte) []byte
	}
	count int64
}

func (w *countingHashWriter) Write(data []byte) (int, error) {
	n, err := w.hash.Write(data)
	w.count += int64(n)
	return n, err
}

func TestGitFilterProtocolSmudgeFraming(t *testing.T) {
	var in bytes.Buffer

	inPL := pktline.NewPktline(nil, &in)
	if err := inPL.WritePacketText("git-filter-client"); err != nil {
		t.Fatalf("write client welcome: %v", err)
	}
	if err := inPL.WritePacketList([]string{"version=2"}); err != nil {
		t.Fatalf("write versions: %v", err)
	}
	if err := inPL.WritePacketList([]string{"capability=clean", "capability=smudge"}); err != nil {
		t.Fatalf("write capabilities: %v", err)
	}
	if err := inPL.WritePacketList([]string{"command=smudge", "pathname=path/test.dat"}); err != nil {
		t.Fatalf("write request headers: %v", err)
	}

	inPayload := pktline.NewPktlineWriter(&in, pktline.MaxPacketLength)
	if _, err := inPayload.Write([]byte("pointer-bytes\n")); err != nil {
		t.Fatalf("write request content: %v", err)
	}
	if err := inPayload.Flush(); err != nil {
		t.Fatalf("flush request content: %v", err)
	}

	var out bytes.Buffer
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	f := NewGitFilter(&in, &out, logger).OnSmudge(func(ctx context.Context, req FilterRequest, ptr io.Reader, dst io.Writer) error {
		gotPayload, err := io.ReadAll(ptr)
		if err != nil {
			t.Fatalf("read pointer payload: %v", err)
		}
		if string(gotPayload) != "pointer-bytes\n" {
			t.Fatalf("unexpected pointer payload: %q", string(gotPayload))
		}
		_, err = dst.Write([]byte("smudged-content"))
		return err
	})

	if err := f.Run(context.Background()); err != nil {
		t.Fatalf("filter run failed: %v", err)
	}

	outPL := pktline.NewPktline(&out, nil)
	serverInit, err := outPL.ReadPacketList()
	if err != nil {
		t.Fatalf("read server init: %v", err)
	}
	if !reflect.DeepEqual(serverInit, []string{"git-filter-server", "version=2"}) {
		t.Fatalf("unexpected server init list: %v", serverInit)
	}
}
