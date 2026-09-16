package filter

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"reflect"
	"testing"

	"github.com/git-lfs/pktline"
)

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
