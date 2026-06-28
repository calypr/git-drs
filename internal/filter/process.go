package filter

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	"github.com/git-lfs/pktline"
)

type SmudgeFunc func(ctx context.Context, req FilterRequest, ptr io.Reader, dst io.Writer) error

type CleanFunc func(ctx context.Context, req FilterRequest, content io.Reader, dst io.Writer) error

type FilterRequest struct {
	Command  string
	Pathname string
}

type GitFilter struct {
	pl     *pktline.Pktline
	out    io.Writer
	smudge SmudgeFunc
	clean  CleanFunc
	logger *slog.Logger
}

func NewGitFilter(in io.Reader, out io.Writer, logger *slog.Logger) *GitFilter {
	return &GitFilter{
		pl:     pktline.NewPktline(in, out),
		out:    out,
		logger: logger,
	}
}

func (f *GitFilter) OnSmudge(fn SmudgeFunc) *GitFilter {
	f.smudge = fn
	return f
}

func (f *GitFilter) OnClean(fn CleanFunc) *GitFilter {
	f.clean = fn
	return f
}

func (f *GitFilter) Run(ctx context.Context) error {
	f.logger.Debug("Starting git filter process")
	if err := f.handshake(); err != nil {
		f.logger.Debug(fmt.Sprintf("Handshake failed: %v", err))
		return fmt.Errorf("git-filter: handshake: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := f.processOne(ctx); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (f *GitFilter) handshake() error {
	initMsg, err := f.pl.ReadPacketText()
	if err != nil {
		return fmt.Errorf("reading welcome: %w", err)
	}
	if initMsg != "git-filter-client" {
		return fmt.Errorf("expected 'git-filter-client', got %q", initMsg)
	}

	versions, err := f.pl.ReadPacketList()
	if err != nil {
		return fmt.Errorf("reading versions: %w", err)
	}
	if !slices.Contains(versions, "version=2") {
		return fmt.Errorf("unsupported filter protocol versions %v (requires version=2)", versions)
	}

	if err := f.pl.WritePacketList([]string{"git-filter-server", "version=2"}); err != nil {
		return err
	}

	if _, err := f.pl.ReadPacketList(); err != nil {
		return fmt.Errorf("reading capabilities: %w", err)
	}

	return f.pl.WritePacketList([]string{"capability=clean", "capability=smudge"})
}

func (f *GitFilter) processOne(ctx context.Context) error {
	f.logger.Debug("Waiting for next filter request...")
	req, err := f.readRequest()
	if err != nil {
		f.logger.Debug(fmt.Sprintf("Error reading filter request: %v", err))
		return err
	}
	f.logger.Debug("Received filter request", "command", req.Command, "pathname", req.Pathname)
	content, err := f.readContent()
	if err != nil {
		return fmt.Errorf("reading content for %s %s: %w", req.Command, req.Pathname, err)
	}

	var handlerErr error
	switch req.Command {
	case "smudge":
		handlerErr = f.handleSmudge(ctx, req, content)
	case "clean":
		handlerErr = f.handleClean(ctx, req, content)
	default:
		handlerErr = fmt.Errorf("unknown command %q", req.Command)
	}

	if handlerErr != nil {
		if err := f.pl.WritePacketList([]string{"status=error"}); err != nil {
			return err
		}
		return nil
	}
	return nil
}

func (f *GitFilter) handleSmudge(ctx context.Context, req FilterRequest, content []byte) error {
	if f.smudge == nil {
		return f.passthroughSmudge(content)
	}

	var dst bytes.Buffer
	if err := f.smudge(ctx, req, bytes.NewReader(content), &dst); err != nil {
		return err
	}
	return f.writeSuccessResponse(dst.Bytes())
}

func (f *GitFilter) handleClean(ctx context.Context, req FilterRequest, content []byte) error {
	if f.clean == nil {
		return f.passthroughClean(content)
	}

	var dst bytes.Buffer
	if err := f.clean(ctx, req, bytes.NewReader(content), &dst); err != nil {
		return err
	}
	return f.writeSuccessResponse(dst.Bytes())
}

func (f *GitFilter) passthroughSmudge(content []byte) error {
	return f.writeSuccessResponse(content)
}

func (f *GitFilter) passthroughClean(content []byte) error {
	return f.writeSuccessResponse(content)
}

func (f *GitFilter) writeSuccessResponse(data []byte) error {
	if err := f.pl.WritePacketList([]string{"status=success"}); err != nil {
		return err
	}

	w := pktline.NewPktlineWriter(f.out, pktline.MaxPacketLength)
	if len(data) > 0 {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}

	return f.pl.WritePacketList(nil)
}

func (f *GitFilter) readRequest() (FilterRequest, error) {
	requestList, err := f.pl.ReadPacketList()
	if err != nil {
		return FilterRequest{}, err
	}

	var req FilterRequest
	for _, line := range requestList {
		if kv := strings.SplitN(line, "=", 2); len(kv) == 2 {
			switch kv[0] {
			case "command":
				req.Command = kv[1]
			case "pathname":
				req.Pathname = kv[1]
			}
		}
	}
	return req, nil
}

func (f *GitFilter) readContent() ([]byte, error) {
	return io.ReadAll(pktline.NewPktlineReaderFromPktline(f.pl, pktline.MaxPacketLength))
}
